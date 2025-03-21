package archive

import "time"

type Config struct {
	Token       string  `yaml:"token"`
	TelegramURL string  `yaml:"telegram_url"`
	Data        string  `yaml:"data"`
	Admins      []int64 `yaml:"admins"`
	Sleep       struct {
		FromHour  int `yaml:"from_hour"`
		UntilHour int `yaml:"until_hour"`
	} `yaml:"sleep"`
	TimeoutMinutes int `yaml:"timeout_minutes"`
}

type UserTg struct {
	ChatId   int64 `yaml:"chat"`
	ThreadId int64 `yaml:"thread"`
}

type User struct {
	Username   string    `yaml:"username"`
	Id         string    `yaml:"id"`
	Tg         []*UserTg `yaml:"tg,omitempty"`
	LastUpdate time.Time `yaml:"last_update,omitempty"`
}

type DownloadedPost struct {
	Id         string    `yaml:"id"`
	Tag        string    `yaml:"tag"`
	Files      []string  `yaml:"files"`
	IsVideo    bool      `yaml:"is_video"`
	CreateTime int64     `yaml:"create_time"`
	DownloadAt time.Time `yaml:"download_at"`
}

type PostError struct {
	PostId  string `yaml:"post_id"`
	UserTag string `yaml:"user_tag"`
	Error   string `yaml:"error"`
}
