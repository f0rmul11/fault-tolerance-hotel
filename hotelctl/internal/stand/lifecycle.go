package stand

import (
	"context"
	"fmt"
)

func (m *Manager) Stop(ctx context.Context) error {
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := m.load(false)
	if err != nil {
		return err
	}
	if err = m.Runtime.Stop(ctx, state); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Стенд остановлен. Постоянные тома сохранены; память Jaeger и очереди Collector очищены. Запуск: hotelctl up.")
	return nil
}

func (m *Manager) Destroy(ctx context.Context) error {
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := m.Store.Load()
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Fprintln(m.Streams.Out, "Stand is already absent.")
		return nil
	}
	if err = m.Runtime.Destroy(ctx, state); err != nil {
		return err
	}
	if err = m.Store.Clear(); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Stand and its data volume deleted.")
	return nil
}
