package store

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// StatsByDomainIndex is the global secondary index of the stats table with
// partition key domain and sort key timestamp. Its name matches the index in
// terraform/modules/data.
const StatsByDomainIndex = "by-domain"

// Dynamo implements every store operation against DynamoDB. A Lambda sets
// only the table names it is granted access to.
type Dynamo struct {
	Client     *dynamodb.Client
	LinksTable string
	StatsTable string
}

func linkKey(domain, slug string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"domain": &types.AttributeValueMemberS{Value: domain},
		"slug":   &types.AttributeValueMemberS{Value: slug},
	}
}

func isConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}

// GetLink returns the link stored under (domain, slug); ok is false when there is none.
func (d *Dynamo) GetLink(ctx context.Context, domain, slug string) (link Link, ok bool, err error) {
	out, err := d.Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.LinksTable),
		Key:       linkKey(domain, slug),
	})
	if err != nil {
		return Link{}, false, err
	}
	if out.Item == nil {
		return Link{}, false, nil
	}
	if _, has := out.Item["target_url"]; !has {
		return Link{}, false, errors.New("link " + LinkKey(domain, slug) + " has no target_url")
	}
	if err := attributevalue.UnmarshalMap(out.Item, &link); err != nil {
		return Link{}, false, err
	}
	return link, true, nil
}

// eachLinkPage calls fn with every page of links of domain, read with a
// Query, or of every domain, read with a Scan, when domain is "". A non-empty
// projection limits the attributes read; it may use #d for "domain", which is
// a DynamoDB reserved word.
func (d *Dynamo) eachLinkPage(ctx context.Context, domain, projection string, fn func([]map[string]types.AttributeValue) error) error {
	names := map[string]string{"#d": "domain"}
	var proj *string
	if projection != "" {
		proj = aws.String(projection)
	}
	if domain == "" {
		input := &dynamodb.ScanInput{TableName: aws.String(d.LinksTable), ProjectionExpression: proj}
		if proj != nil {
			input.ExpressionAttributeNames = names
		}
		p := dynamodb.NewScanPaginator(d.Client, input)
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				return err
			}
			if err := fn(page.Items); err != nil {
				return err
			}
		}
		return nil
	}
	p := dynamodb.NewQueryPaginator(d.Client, &dynamodb.QueryInput{
		TableName:                 aws.String(d.LinksTable),
		KeyConditionExpression:    aws.String("#d = :d"),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: map[string]types.AttributeValue{":d": &types.AttributeValueMemberS{Value: domain}},
		ProjectionExpression:      proj,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		if err := fn(page.Items); err != nil {
			return err
		}
	}
	return nil
}

// ListLinks returns the links of domain ordered by slug (a Query), or every
// link in scan order when domain is "" (a Scan over the whole table).
func (d *Dynamo) ListLinks(ctx context.Context, domain string) ([]Link, error) {
	links := []Link{}
	err := d.eachLinkPage(ctx, domain, "", func(items []map[string]types.AttributeValue) error {
		var batch []Link
		if err := attributevalue.UnmarshalListOfMaps(items, &batch); err != nil {
			return err
		}
		links = append(links, batch...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return links, nil
}

// ListLinkKeys returns the stats key (see LinkKey) of every link of domain,
// or of every link when domain is "".
func (d *Dynamo) ListLinkKeys(ctx context.Context, domain string) ([]string, error) {
	var keys []string
	err := d.eachLinkPage(ctx, domain, "#d, slug", func(items []map[string]types.AttributeValue) error {
		var rows []struct {
			Domain string `dynamodbav:"domain"`
			Slug   string `dynamodbav:"slug"`
		}
		if err := attributevalue.UnmarshalListOfMaps(items, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			keys = append(keys, LinkKey(row.Domain, row.Slug))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// CreateLink stores link unless its slug is taken on its domain (ErrLinkExists).
func (d *Dynamo) CreateLink(ctx context.Context, link Link) error {
	item, err := attributevalue.MarshalMap(link)
	if err != nil {
		return err
	}
	_, err = d.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(d.LinksTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(slug)"),
	})
	if isConditionFailed(err) {
		return ErrLinkExists
	}
	return err
}

// UpdateLinkTarget sets target_url and updated_at of an existing link and
// returns the whole updated row (ErrLinkNotFound when it does not exist).
func (d *Dynamo) UpdateLinkTarget(ctx context.Context, domain, slug, targetURL, updatedAt string) (Link, error) {
	out, err := d.Client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(d.LinksTable),
		Key:                 linkKey(domain, slug),
		UpdateExpression:    aws.String("SET target_url = :url, updated_at = :now"),
		ConditionExpression: aws.String("attribute_exists(slug)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":url": &types.AttributeValueMemberS{Value: targetURL},
			":now": &types.AttributeValueMemberS{Value: updatedAt},
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if isConditionFailed(err) {
		return Link{}, ErrLinkNotFound
	}
	if err != nil {
		return Link{}, err
	}
	var link Link
	if err := attributevalue.UnmarshalMap(out.Attributes, &link); err != nil {
		return Link{}, err
	}
	return link, nil
}

// DeleteLink removes an existing link (ErrLinkNotFound when it does not exist).
func (d *Dynamo) DeleteLink(ctx context.Context, domain, slug string) error {
	_, err := d.Client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           aws.String(d.LinksTable),
		Key:                 linkKey(domain, slug),
		ConditionExpression: aws.String("attribute_exists(slug)"),
	})
	if isConditionFailed(err) {
		return ErrLinkNotFound
	}
	return err
}

// PutClick writes one click row.
func (d *Dynamo) PutClick(ctx context.Context, click Click) error {
	item, err := attributevalue.MarshalMap(click)
	if err != nil {
		return err
	}
	_, err = d.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.StatsTable),
		Item:      item,
	})
	return err
}

// rangeCondition returns the timestamp condition for r ("" when r is open)
// and adds its placeholders to names and values. "timestamp" is a DynamoDB
// reserved word, hence #ts.
func rangeCondition(r TimeRange, names map[string]string, values map[string]types.AttributeValue) string {
	var cond string
	switch {
	case r.Start != "" && r.End != "":
		cond = "#ts BETWEEN :start AND :end"
	case r.Start != "":
		cond = "#ts >= :start"
	case r.End != "":
		cond = "#ts <= :end"
	default:
		return ""
	}
	names["#ts"] = "timestamp"
	if r.Start != "" {
		values[":start"] = &types.AttributeValueMemberS{Value: r.Start}
	}
	if r.End != "" {
		values[":end"] = &types.AttributeValueMemberS{Value: r.End}
	}
	return cond
}

// ScanClicks returns every click of every domain within r, in scan order.
// It reads the whole stats table; the range is applied as a filter, so it
// narrows the result but not the read cost.
func (d *Dynamo) ScanClicks(ctx context.Context, r TimeRange) ([]Click, error) {
	names := map[string]string{}
	values := map[string]types.AttributeValue{}
	input := &dynamodb.ScanInput{TableName: aws.String(d.StatsTable)}
	if cond := rangeCondition(r, names, values); cond != "" {
		input.FilterExpression = aws.String(cond)
		input.ExpressionAttributeNames = names
		input.ExpressionAttributeValues = values
	}

	var clicks []Click
	p := dynamodb.NewScanPaginator(d.Client, input)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var batch []Click
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &batch); err != nil {
			return nil, err
		}
		clicks = append(clicks, batch...)
	}
	return clicks, nil
}

// QueryDomainClicks returns the clicks of every link on domain within r,
// oldest first, from the by-domain index.
func (d *Dynamo) QueryDomainClicks(ctx context.Context, domain string, r TimeRange) ([]Click, error) {
	return d.queryClicks(ctx, aws.String(StatsByDomainIndex), "#d", "domain", domain, r)
}

// QueryLinkClicks returns the clicks of one link within r, oldest first.
func (d *Dynamo) QueryLinkClicks(ctx context.Context, domain, slug string, r TimeRange) ([]Click, error) {
	return d.queryClicks(ctx, nil, "#k", "link_key", LinkKey(domain, slug), r)
}

func (d *Dynamo) queryClicks(ctx context.Context, index *string, keyName, keyAttr, keyValue string, r TimeRange) ([]Click, error) {
	names := map[string]string{keyName: keyAttr}
	values := map[string]types.AttributeValue{":key": &types.AttributeValueMemberS{Value: keyValue}}
	cond := keyName + " = :key"
	if rc := rangeCondition(r, names, values); rc != "" {
		cond += " AND " + rc
	}
	input := &dynamodb.QueryInput{
		TableName:                 aws.String(d.StatsTable),
		IndexName:                 index,
		KeyConditionExpression:    aws.String(cond),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	}

	var clicks []Click
	p := dynamodb.NewQueryPaginator(d.Client, input)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var batch []Click
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &batch); err != nil {
			return nil, err
		}
		clicks = append(clicks, batch...)
	}
	return clicks, nil
}
