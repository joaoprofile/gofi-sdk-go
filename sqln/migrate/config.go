package migrate

import "embed"

type Config struct {
	Path string
	FS   embed.FS
}
