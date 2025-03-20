package archive

import (
	"fmt"
	"github.com/cavaliergopher/grab/v3"
	"github.com/goccy/go-yaml"
	"github.com/kittenbark/nanodb"
	"github.com/kittenbark/tg"
	"github.com/kittenbark/tikwm/lib"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"path"
	"time"
)

type Archive struct {
	cfg        *Config
	tg         *tg.Bot
	users      *nanodb.DBCache[*User, *yaml.Encoder, *yaml.Decoder]
	downloaded *nanodb.DBCache[*DownloadedPost, *yaml.Encoder, *yaml.Decoder]
	errors     *nanodb.DBCache[*PostError, *yaml.Encoder, *yaml.Decoder]
}

func New(config, archive, downloaded, errors string) (*Archive, error) {
	var cfg Config
	data, err := os.ReadFile(config)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	db, err := nanodb.Fromf[*User](archive, yamlNewEncoder, yamlNewDecoder)
	if err != nil {
		return nil, fmt.Errorf("users: open archive, %w", err)
	}

	downloadedDb, err := nanodb.Fromf[*DownloadedPost](downloaded, yamlNewEncoder, yamlNewDecoder)
	if err != nil {
		return nil, fmt.Errorf("users: open downloaded, %w", err)
	}

	errorsDb, err := nanodb.Fromf[*PostError](errors, yamlNewEncoder, yamlNewDecoder)
	if err != nil {
		return nil, fmt.Errorf("users: open errors, %w", err)
	}

	bot := tg.New(&tg.Config{Token: cfg.Token, ApiURL: cfg.TelegramURL, TimeoutHandle: -1})
	if _, err := tg.GetMe(bot.Context()); err != nil {
		return nil, fmt.Errorf("get me: %w", err)
	}

	grab.DefaultClient.UserAgent = "Mozilla/5.0 (Linux; Android 14; RMX3710 Build/UKQ1.230924.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/133.0.6943.137 Mobile Safari/537.36 trill_390003 BytedanceWebview/d8a21c6"
	return &Archive{&cfg, bot, db, downloadedDb, errorsDb}, nil
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
	slog.Debug("archive#DownloadUser", "tag", tag, "user", user.Id)
	for post, err := range tikwm.FeedSeq(user.Id) {
		if err != nil || post == nil {
			arch.newError(post, tag, fmt.Errorf("tikwm: get user post, %w (%s)", err, tag))
			continue
		}
		if time.Unix(post.CreateTime, 0).Before(user.LastUpdate) {
			break
		}
		content := post.ContentUrls()
		if len(content) == 0 {
			arch.newError(post, tag, fmt.Errorf("tikwm: get user post, missing content urls (%s, %s)", tag, post.Id))
			continue
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
				arch.newError(post, tag, fmt.Errorf("grab: get user post, %w (%s, %s)", err, tag, post.Id))
				continue
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
				arch.newError(post, tag, fmt.Errorf("grab: get user post, %w (%s)", err, tag))
				continue
			}
			pictures = append(pictures, filename)
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

func (arch *Archive) newError(post *tikwm.UserPost, tag string, err error) {
	if post == nil {
		post = &tikwm.UserPost{Id: fmt.Sprintf("%s_%s", tag, time.Now().Format("20060102150405"))}
	}
	slog.Error("archive#user", "post", post.Id, "tag", tag, "err", err)

	arch.newErrorId(post.Id, tag, err)
}

func (arch *Archive) newErrorId(postId string, tag string, err error) {
	if err := arch.errors.Add(postId, &PostError{
		PostId:  postId,
		UserTag: tag,
		Error:   fmt.Sprintf("tikwm: get user post, %v (%s, %s)", err, tag, postId),
	}); err != nil {
		slog.Error("archive#new_error_write_error", "post", postId, "tag", tag, "err", err)
	}
	time.Sleep(time.Second * 10)
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

func yamlNewEncoder(w io.Writer) *yaml.Encoder {
	return yaml.NewEncoder(w)
}

func yamlNewDecoder(r io.Reader) *yaml.Decoder {
	return yaml.NewDecoder(r)
}
