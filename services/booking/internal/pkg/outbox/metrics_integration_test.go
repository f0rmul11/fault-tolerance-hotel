package outbox

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"booking/internal/pkg/event"
	"booking/internal/pkg/transaction"
)

type metricsHandler struct {
	topic string
	err   error
}

func (h *metricsHandler) Topic() string               { return h.topic }
func (*metricsHandler) BatchSize(context.Context) int { return 4 }
func (*metricsHandler) PartitionsCount() int32        { return 2 }
func (*metricsHandler) Partition() (int32, bool)      { return 0, true }
func (h *metricsHandler) HandlePartition(context.Context, int32, event.Events) error {
	return h.err
}

func TestMetricsWithPostgres(t *testing.T) {
	dsn := os.Getenv("BOOKING_TEST_DSN")
	if dsn == "" {
		t.Skip("set BOOKING_TEST_DSN to a migrated disposable PostgreSQL")
	}

	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	topic := "outbox-metrics-" + uuid.Must(uuid.NewV4()).String()
	_, err = pool.Exec(ctx, "insert into public.outbox_offsets (topic, partition_id) values ($1, 0), ($1, 1)", topic)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "delete from public.outbox_messages where topic = $1", topic)
		require.NoError(t, err)
		_, err = pool.Exec(context.Background(), "delete from public.outbox_offsets where topic = $1", topic)
		require.NoError(t, err)
		pendingMessages.DeleteLabelValues(topic)
		messagesWritten.DeleteLabelValues(topic)
		messagesProcessed.DeleteLabelValues(topic)
		failedBatches.DeleteLabelValues(topic)
	})

	handler := &metricsHandler{topic: topic, err: errors.New("publish failed")}
	box := NewOutbox(pool, Config{MessagesLimit: 4})
	box.RegisterHandler(handler)

	pending := pendingMessages.WithLabelValues(topic)
	written := messagesWritten.WithLabelValues(topic)
	processed := messagesProcessed.WithLabelValues(topic)
	failed := failedBatches.WithLabelValues(topic)

	box.SendMetrics(ctx)
	require.Zero(t, testutil.ToFloat64(pending))
	require.Zero(t, testutil.ToFloat64(written))
	require.Zero(t, testutil.ToFloat64(processed))
	require.Zero(t, testutil.ToFloat64(failed))

	err = transaction.Exec(ctx, func(ctx context.Context) error {
		return box.Flush(ctx, event.Events{{Schema: topic, Key: []byte("key"), Body: []byte("body")}})
	})
	require.NoError(t, err)
	box.SendMetrics(ctx)
	require.EqualValues(t, 1, testutil.ToFloat64(written))
	require.EqualValues(t, 1, testutil.ToFloat64(pending))

	// Other integration tests may briefly hold an older transaction ID.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		var eligible bool
		require.NoError(t, pool.QueryRow(ctx, `
			select transaction_id < pg_snapshot_xmin(pg_current_snapshot())
			from public.outbox_messages
			where topic = $1
		`, topic).Scan(&eligible))
		require.True(t, eligible)
	}, 5*time.Second, 10*time.Millisecond)

	box.HandlePendingMessages(ctx)
	box.SendMetrics(ctx)
	require.EqualValues(t, 1, testutil.ToFloat64(failed))
	require.EqualValues(t, 1, testutil.ToFloat64(pending))
	require.Zero(t, testutil.ToFloat64(processed))

	handler.err = nil
	box.HandlePendingMessages(ctx)
	box.SendMetrics(ctx)
	require.EqualValues(t, 1, testutil.ToFloat64(processed))
	require.EqualValues(t, 1, testutil.ToFloat64(failed))
	require.Zero(t, testutil.ToFloat64(pending))

	// Each partition is below the cap. The combined topic backlog exceeds it.
	_, err = pool.Exec(ctx, `
		insert into public.outbox_messages (topic, partition_id, key, body)
		select $1, i % 2, '\x01'::bytea, '\x02'::bytea
		from generate_series(1, $2::integer) i
	`, topic, pendingMetricLimit+2)
	require.NoError(t, err)
	box.SendMetrics(ctx)
	require.EqualValues(t, pendingMetricLimit, testutil.ToFloat64(pending))
}
