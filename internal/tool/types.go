package tool

import (
	"context"
	"errors"
)

type Description struct {
	Name        string
	DisplayName string
	Description string
	Input       string
}

type Tool interface {
	Description() Description
	Call(ctx context.Context, input string) (string, error)
}

type Func struct {
	Desc Description
	Fn   func(ctx context.Context, input string) (string, error)
}

func (f Func) Description() Description {
	return f.Desc
}

func (f Func) Call(ctx context.Context, input string) (string, error) {
	if f.Fn == nil {
		return "", errors.New("tool: nil function")
	}
	return f.Fn(ctx, input)
}
