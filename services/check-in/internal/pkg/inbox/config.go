package inbox

import "time"

type Config struct {
	// Максимальное количество сообщений (в сумме), которые может получить один воркер по заблокированным ключам
	MessagesLimit int `yaml:"messages_limit"`
	// Количество ошибок обработки одного сообщения, после которого, будет стрелять алерт
	MaxErrCountForMessage int `yaml:"max_err_count_for_message"`

	// Интервал опроса обычных сообщений
	NormalMessagesPollInterval time.Duration `yaml:"normal_messages_poll_interval"`
	// Интервал опроса ошибочных сообщений
	ErrorMessagesPollInterval time.Duration `yaml:"error_messages_poll_interval"`
}
