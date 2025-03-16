package archive

import (
	"context"
	"fmt"
	"github.com/cavaliergopher/grab/v3"
	"github.com/kittenbark/nanodb"
	"github.com/kittenbark/tg"
	"github.com/kittenbark/tikwm/lib"
	"gopkg.in/yaml.v3"
	"log/slog"
	"math/rand"
	"os"
	"path"
	"slices"
	"time"
)

type Archive struct {
	cfg        *Config
	tg         *tg.Bot
	users      *nanodb.DBCache[*User, *yaml.Encoder, *yaml.Decoder]
	downloaded *nanodb.DBCache[*DownloadedPost, *yaml.Encoder, *yaml.Decoder]
}

func New(config string, archive string, downloaded string) (*Archive, error) {
	var cfg Config
	data, err := os.ReadFile(config)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	db, err := nanodb.Fromf[*User](archive, yaml.NewEncoder, yaml.NewDecoder)
	if err != nil {
		return nil, fmt.Errorf("users: open archive, %w", err)
	}

	downloadedDb, err := nanodb.Fromf[*DownloadedPost](downloaded, yaml.NewEncoder, yaml.NewDecoder)
	if err != nil {
		return nil, fmt.Errorf("users: open downloaded, %w", err)
	}

	bot := tg.New(&tg.Config{Token: cfg.Token, ApiURL: cfg.TelegramURL, TimeoutHandle: -1})
	if _, err := tg.GetMe(bot.Context()); err != nil {
		return nil, fmt.Errorf("get me: %w", err)
	}

	grab.DefaultClient.UserAgent = "Mozilla/5.0 (Linux; Android 14; RMX3710 Build/UKQ1.230924.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/133.0.6943.137 Mobile Safari/537.36 trill_390003 BytedanceWebview/d8a21c6"
	return &Archive{&cfg, bot, db, downloadedDb}, nil
}

func (arch *Archive) Start() {
	go arch.StartBot()

	slog.Info("archive#start")
	ticker := time.NewTicker(time.Minute * time.Duration(arch.cfg.TimeoutMinutes))
	for ; ; <-ticker.C {
		start := time.Now()
		tags, err := arch.users.KeysSnapshot()
		if err != nil {
			slog.Error("archive#keys_snapshot", "err", err)
			continue
		}

		for _, tag := range tags {
			if err := arch.DownloadUser(tag); err != nil {
				slog.Error("archive#user", "tag", tag, "err", err)
				continue
			}
			if err := arch.UploadTg(); err != nil {
				slog.Error("archive#upload", "tag", tag, "err", err)
				continue
			}
		}

		slog.Info("archive#done", "elapsed", time.Since(start))
	}
}

func (arch *Archive) DownloadUser(tag string) error {
	user, err := arch.users.Get(tag)
	if err != nil {
		return fmt.Errorf("users: add user, %w (%s)", err, tag)
	}

	if user.Id == "" {
		details, err := tikwm.Details(user.Username)
		if err != nil {
			return fmt.Errorf("tiwkm: get user details, %w (%s)", err, tag)
		}
		user.Id = details.User.Id
		if err := arch.users.Add(tag, user); err != nil {
			return fmt.Errorf("users: add user, %w (%s)", err, tag)
		}
		if err := os.MkdirAll(path.Join(arch.cfg.Data, tag), os.ModePerm); err != nil {
			return fmt.Errorf("mkdir: mkdirall, %w (%s)", err, tag)
		}
	}

	lastUpdate := time.Now()
	for post, err := range tikwm.FeedSeq(user.Id) {
		if err != nil {
			return fmt.Errorf("tikwm: get user post, %w (%s)", err, tag)
		}
		if time.Unix(post.CreateTime, 0).Before(user.LastUpdate) {
			break
		}
		content := post.ContentUrls()
		if len(content) == 0 {
			return fmt.Errorf("tikwm: get user post, missing content urls (%s, %s)", tag, post.Id)
		}

		slog.Debug(
			"archive#user_download",
			"tag", tag,
			"post", post.Id,
			"create_time", time.Unix(post.CreateTime, 0).String(),
		)
		if post.IsVideo() && len(content) > 0 {
			filename := arch.videoPath(tag, user.Username, post)
			if _, err := grab.Get(filename, content[0]); err != nil {
				return fmt.Errorf("grab: get user post, %w (%s, %s)", err, tag, post.Id)
			}
			if err := arch.downloaded.Add(post.Id, &DownloadedPost{
				Id:         post.Id,
				Tag:        tag,
				Files:      []string{filename},
				IsVideo:    true,
				CreateTime: post.CreateTime,
				DownloadAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("tg: add post, %w (%s)", err, tag)
			}

			continue
		}

		pictures := make([]string, 0)
		for i := range content {
			filename := arch.picturePath(tag, user.Username, post, i)
			if _, err := grab.Get(filename, content[i]); err != nil {
				return fmt.Errorf("grab: get user post, %w (%s)", err, tag)
			}
			time.Sleep(time.Millisecond * time.Duration(100+rand.Intn(200)))
		}
		if err := arch.downloaded.Add(post.Id, &DownloadedPost{
			Id:         post.Id,
			Tag:        tag,
			Files:      pictures,
			IsVideo:    false,
			CreateTime: post.CreateTime,
			DownloadAt: time.Now(),
		}); err != nil {
			return fmt.Errorf("tg: add post, %w (%s)", err, tag)
		}
	}

	user.LastUpdate = lastUpdate
	if err := arch.users.Add(tag, user); err != nil {
		return fmt.Errorf("users: add user, %w (%s)", err, tag)
	}

	return nil
}

func (arch *Archive) videoPath(tag string, username string, post *tikwm.UserPost) string {
	return path.Join(
		arch.cfg.Data,
		tag,
		fmt.Sprintf(
			"%s_%s_%s.mp4",
			username,
			time.Unix(post.CreateTime, 0).Format(time.DateOnly),
			post.Id,
		),
	)
}

func (arch *Archive) picturePath(tag string, username string, post *tikwm.UserPost, i int) string {
	return path.Join(
		arch.cfg.Data,
		tag,
		fmt.Sprintf(
			"%s_%s_%s_%d.jpg",
			username,
			time.Unix(post.CreateTime, 0).Format(time.DateOnly),
			post.Id,
			i,
		),
	)
}

func (arch *Archive) onAdmin(ctx context.Context, upd *tg.Update) bool {
	return tg.OnMessage(ctx, upd) && upd.Message.From != nil && slices.Contains(arch.cfg.Admins, upd.Message.From.Id)
}
