package global

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"api-gateway/internal/pkg/closer"
	"api-gateway/internal/pkg/rtconfig"
	"api-gateway/internal/pkg/rtconfig/system"
)

// Init явно создаёт общий клиент до запуска обработчиков приложения.
// Здесь же подключаются системные callbacks и закрытие через общий closer.
// Вызывается один раз при старте; Client и GetValue сами сеть не запускают.
func Init(ctx context.Context, cfg rtconfig.Config, schemaHash string) (*rtconfig.Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	schema, err := loadSchema(cfg.SchemaPath, schemaHash)
	if err != nil {
		return nil, err
	}
	manager, err := rtconfig.New(schema, rtconfig.Options{
		Endpoints:      cfg.Endpoints,
		Prefix:         cfg.Prefix,
		StartupTimeout: cfg.StartupTimeout,
	})
	if err != nil {
		return nil, err
	}
	// Закрытие использует свой контекст: lifetime к этому моменту может быть отменён.
	closeClient := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		return manager.Close(ctx)
	}

	previous := Client()
	SetClient(manager)
	if err := start(ctx, manager, cfg.StartupTimeout); err != nil {
		SetClient(previous)
		return nil, errors.Join(err, closeClient())
	}
	closer.Add(closeClient)

	return manager, nil
}

func loadSchema(path, expectedHash string) (*rtconfig.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read runtime schema: %w", err)
	}
	service, err := rtconfig.ParseSchema(data)
	if err != nil {
		return nil, fmt.Errorf("parse runtime schema: %w", err)
	}
	system.Freeze()
	schema, err := rtconfig.NewSchema(append(service.Definitions(), system.Definitions()...)...)
	if err != nil {
		return nil, fmt.Errorf("merge runtime schema: %w", err)
	}
	if schema.Hash() != expectedHash {
		return nil, fmt.Errorf("runtime schema differs from generated facade; run go generate ./internal/config and rebuild")
	}
	return schema, nil
}

func start(ctx context.Context, manager *rtconfig.Manager, timeout time.Duration) error {
	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("start runtime config: %w", err)
	}
	callbacks := system.Callbacks()
	names := make([]string, 0, len(callbacks))
	for name := range callbacks {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		callback := callbacks[name]
		if err := manager.WatchVariable(ctx, name, func(old, next rtconfig.Variable) {
			callback(old.Value(), next.Value())
		}); err != nil {
			return fmt.Errorf("register runtime callback %q: %w", name, err)
		}
	}
	initial, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := manager.WaitForInitial(initial); err != nil {
		return fmt.Errorf("apply initial runtime callbacks: %w", err)
	}
	return nil
}
