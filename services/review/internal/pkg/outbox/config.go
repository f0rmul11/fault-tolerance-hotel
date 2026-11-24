package outbox

type Config struct {
	// Максимальное количество ключей (столбец `entity_id` в `outbox`) которое может быть заблокировано одним обработчиком
	LockedKeysLimit int `yaml:"locked_keys_limit"`
	// Максимальное количество сообщений (в сумме), которые может получить один воркер по заблокированным ключам
	MessagesLimit int `yaml:"messages_limit"`
	// Количество ошибок обработки одного сообщения, после которого, будет стрелять алерт
	MaxErrCountForMessage int `yaml:"max_err_count_for_message"`
}
