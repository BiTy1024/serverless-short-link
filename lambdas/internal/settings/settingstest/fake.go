// Package settingstest provides an in-memory SSM Parameter Store for tests.
package settingstest

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
)

// pageSize is the GetParametersByPath page size, as small as SSM's maximum of
// 10 so paging is exercised.
const pageSize = 10

// SSM keeps parameters by name. It mirrors the SSM semantics the store relies
// on: ParameterNotFound, ParameterAlreadyExists without overwrite, and
// GetParametersByPath returning the direct children of a path in pages.
type SSM struct {
	mu     sync.Mutex
	params map[string]string

	// Err makes every call fail with this error while it is set.
	Err error
	// Calls counts calls per operation name.
	Calls map[string]int
}

// New returns an empty parameter store.
func New() *SSM {
	return &SSM{params: map[string]string{}, Calls: map[string]int{}}
}

// Store returns a settings store at path backed by f.
func (f *SSM) Store(path string) *settings.Store {
	return &settings.Store{Client: f, Path: path}
}

// SetRaw stores value under name as it is.
func (f *SSM) SetRaw(name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.params[name] = value
}

// SetErr sets or clears the error every call fails with.
func (f *SSM) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Err = err
}

// Count returns how often op was called.
func (f *SSM) Count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls[op]
}

// Names returns the stored parameter names in order.
func (f *SSM) Names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.params))
	for n := range f.params {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Put stores d under path the way settings.Store.Put does.
func (f *SSM) Put(path string, d settings.Domain) {
	b, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	f.SetRaw(strings.TrimSuffix(path, "/")+"/"+d.Domain, string(b))
}

func (f *SSM) enter(op string) error {
	f.Calls[op]++
	return f.Err
}

func (f *SSM) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetParameter"); err != nil {
		return nil, err
	}
	name := aws.ToString(in.Name)
	v, ok := f.params[name]
	if !ok {
		return nil, &types.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{Parameter: &types.Parameter{Name: in.Name, Value: aws.String(v)}}, nil
}

func (f *SSM) PutParameter(_ context.Context, in *ssm.PutParameterInput, _ ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("PutParameter"); err != nil {
		return nil, err
	}
	name := aws.ToString(in.Name)
	if _, ok := f.params[name]; ok && !aws.ToBool(in.Overwrite) {
		return nil, &types.ParameterAlreadyExists{}
	}
	f.params[name] = aws.ToString(in.Value)
	return &ssm.PutParameterOutput{}, nil
}

func (f *SSM) DeleteParameter(_ context.Context, in *ssm.DeleteParameterInput, _ ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("DeleteParameter"); err != nil {
		return nil, err
	}
	name := aws.ToString(in.Name)
	if _, ok := f.params[name]; !ok {
		return nil, &types.ParameterNotFound{}
	}
	delete(f.params, name)
	return &ssm.DeleteParameterOutput{}, nil
}

func (f *SSM) GetParametersByPath(_ context.Context, in *ssm.GetParametersByPathInput, _ ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetParametersByPath"); err != nil {
		return nil, err
	}
	prefix := aws.ToString(in.Path) + "/"
	var names []string
	for n := range f.params {
		if rest, ok := strings.CutPrefix(n, prefix); ok && !strings.Contains(rest, "/") {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	start := 0
	if in.NextToken != nil {
		start = slices.Index(names, aws.ToString(in.NextToken))
	}
	end := min(start+pageSize, len(names))
	out := &ssm.GetParametersByPathOutput{}
	for _, n := range names[start:end] {
		out.Parameters = append(out.Parameters, types.Parameter{Name: aws.String(n), Value: aws.String(f.params[n])})
	}
	if end < len(names) {
		out.NextToken = aws.String(names[end])
	}
	return out, nil
}
