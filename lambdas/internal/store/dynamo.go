package store

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Dynamo implements every store operation against DynamoDB. A Lambda sets
// only the table names it is granted access to.
type Dynamo struct {
	Client     *dynamodb.Client
	LinksTable string
	StatsTable string
}

func linkKey(shortPath string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"short_path": &types.AttributeValueMemberS{Value: shortPath},
	}
}

func isConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}

// GetLink returns the link stored under shortPath; ok is false when there is none.
func (d *Dynamo) GetLink(ctx context.Context, shortPath string) (link Link, ok bool, err error) {
	out, err := d.Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.LinksTable),
		Key:       linkKey(shortPath),
	})
	if err != nil {
		return Link{}, false, err
	}
	if out.Item == nil {
		return Link{}, false, nil
	}
	if _, has := out.Item["target_url"]; !has {
		return Link{}, false, errors.New("link " + shortPath + " has no target_url")
	}
	if err := attributevalue.UnmarshalMap(out.Item, &link); err != nil {
		return Link{}, false, err
	}
	return link, true, nil
}

// ListLinks returns every link, in scan order.
func (d *Dynamo) ListLinks(ctx context.Context) ([]Link, error) {
	links := []Link{}
	p := dynamodb.NewScanPaginator(d.Client, &dynamodb.ScanInput{
		TableName: aws.String(d.LinksTable),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var batch []Link
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &batch); err != nil {
			return nil, err
		}
		links = append(links, batch...)
	}
	return links, nil
}

// ListLinkPaths returns the short path of every link, without a leading slash.
func (d *Dynamo) ListLinkPaths(ctx context.Context) ([]string, error) {
	var paths []string
	p := dynamodb.NewScanPaginator(d.Client, &dynamodb.ScanInput{
		TableName:            aws.String(d.LinksTable),
		ProjectionExpression: aws.String("short_path"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			var row struct {
				ShortPath string `dynamodbav:"short_path"`
			}
			if err := attributevalue.UnmarshalMap(item, &row); err != nil {
				return nil, err
			}
			paths = append(paths, row.ShortPath)
		}
	}
	return paths, nil
}

// CreateLink stores link unless its short path is taken (ErrLinkExists).
func (d *Dynamo) CreateLink(ctx context.Context, link Link) error {
	item, err := attributevalue.MarshalMap(link)
	if err != nil {
		return err
	}
	_, err = d.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(d.LinksTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(short_path)"),
	})
	if isConditionFailed(err) {
		return ErrLinkExists
	}
	return err
}

// UpdateLinkTarget sets target_url and updated_at of an existing link and
// returns the whole updated row (ErrLinkNotFound when it does not exist).
func (d *Dynamo) UpdateLinkTarget(ctx context.Context, shortPath, targetURL, updatedAt string) (Link, error) {
	out, err := d.Client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(d.LinksTable),
		Key:                 linkKey(shortPath),
		UpdateExpression:    aws.String("SET target_url = :url, updated_at = :now"),
		ConditionExpression: aws.String("attribute_exists(short_path)"),
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
func (d *Dynamo) DeleteLink(ctx context.Context, shortPath string) error {
	_, err := d.Client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           aws.String(d.LinksTable),
		Key:                 linkKey(shortPath),
		ConditionExpression: aws.String("attribute_exists(short_path)"),
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

// ScanClicks returns every click row, in scan order.
func (d *Dynamo) ScanClicks(ctx context.Context) ([]Click, error) {
	var clicks []Click
	p := dynamodb.NewScanPaginator(d.Client, &dynamodb.ScanInput{
		TableName: aws.String(d.StatsTable),
	})
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

// QueryClicks returns the clicks of one redirect path within r, oldest first.
func (d *Dynamo) QueryClicks(ctx context.Context, redirectPath string, r TimeRange) ([]Click, error) {
	cond := "redirect_path = :path"
	values := map[string]types.AttributeValue{
		":path": &types.AttributeValueMemberS{Value: redirectPath},
	}
	switch {
	case r.Start != "" && r.End != "":
		cond += " AND #ts BETWEEN :start AND :end"
	case r.Start != "":
		cond += " AND #ts >= :start"
	case r.End != "":
		cond += " AND #ts <= :end"
	}
	if r.Start != "" {
		values[":start"] = &types.AttributeValueMemberS{Value: r.Start}
	}
	if r.End != "" {
		values[":end"] = &types.AttributeValueMemberS{Value: r.End}
	}
	input := &dynamodb.QueryInput{
		TableName:                 aws.String(d.StatsTable),
		KeyConditionExpression:    aws.String(cond),
		ExpressionAttributeValues: values,
	}
	if r.Start != "" || r.End != "" {
		// "timestamp" is a DynamoDB reserved word.
		input.ExpressionAttributeNames = map[string]string{"#ts": "timestamp"}
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
