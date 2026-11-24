package pipe

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testObject struct{}

func (t *testObject) Do(err error) func(_ context.Context, value int) (int, error) {
	return func(_ context.Context, value int) (int, error) {
		return value * value, err
	}
}

func TestPipe(t *testing.T) {
	t.Parallel()

	t.Run("успешный ответ", func(t *testing.T) {
		t.Parallel()
		test := &testObject{}
		result, err := With(test.Do(nil)).With(test.Do(nil)).Run(context.Background(), 2).Get()

		assert.NoError(t, err)
		assert.Equal(t, 16, result)
	})

	t.Run("ошибка в одной из секций", func(t *testing.T) {
		t.Parallel()
		test := &testObject{}
		result, err := With(test.Do(fmt.Errorf("err"))).
			With(test.Do(nil)).Run(context.Background(), 2).Get()

		assert.EqualError(t, err, "err")
		assert.Equal(t, 4, result)
	})
}
