// Package global хранит явно установленный runtime client для generated facade.
// Сеть и lifecycle остаются во владении приложения.
package global

import (
	"context"
	"reflect"
	"sync/atomic"

	"api-gateway/internal/pkg/rtconfig"
)

type clientHolder struct {
	client rtconfig.Client
}

var runtimeClient atomic.Pointer[clientHolder]

// Client возвращает явно установленный клиент; получение не создаёт сеть.
func Client() rtconfig.Client {
	if holder := runtimeClient.Load(); holder != nil {
		return holder.client
	}

	return notInitializedClient{}
}

// SetClient устанавливает клиент до начала serving. Подмена global client в тесте
// несовместима с t.Parallel; для независимых тестов используйте generated NewClient.
// Метод не закрывает прежний клиент: его lifecycle принадлежит вызывающему коду.
func SetClient(c rtconfig.Client) {
	if c == nil {
		panic("config: nil realtime config client")
	}

	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			panic("config: nil realtime config client")
		}
	}

	runtimeClient.Store(&clientHolder{client: c})
}

type notInitializedClient struct{}

func (notInitializedClient) Value(context.Context, string) (rtconfig.Value, error) {
	return rtconfig.NewNilValue(rtconfig.ErrNotInitialized), rtconfig.ErrNotInitialized
}

func (notInitializedClient) Values(context.Context) (map[string]rtconfig.Value, error) {
	return nil, rtconfig.ErrNotInitialized
}

func (notInitializedClient) Version() rtconfig.Version {
	return notInitializedVersion{}
}

func (notInitializedClient) Ready(context.Context) <-chan error {
	ready := make(chan error, 1)
	ready <- rtconfig.ErrNotInitialized
	close(ready)

	return ready
}

func (notInitializedClient) WatchVariable(context.Context, string, rtconfig.VariableChangeCallback) error {
	return rtconfig.ErrNotInitialized
}

func (notInitializedClient) WatchAllVariables(context.Context, rtconfig.VariableChangeCallback) error {
	return rtconfig.ErrNotInitialized
}

type notInitializedVersion struct{}

func (notInitializedVersion) ID() string {
	return ""
}

func (notInitializedVersion) ProjectID() string {
	return ""
}

func (notInitializedVersion) IsReady() bool {
	return false
}
