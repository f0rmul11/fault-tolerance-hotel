package outbox

type Config struct {
	// Верхняя граница размера пачки для одной партиции за тик воркера.
	MessagesLimit int `yaml:"messages_limit"`
}
