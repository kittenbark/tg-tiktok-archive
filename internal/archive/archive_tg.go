package archive

import (
	"cmp"
	"context"
	"fmt"
	"github.com/kittenbark/tg"
	"github.com/kittenbark/tgmedia/tgvideo"
	"log/slog"
	"slices"
	"strings"
	"time"
)

type TgArchive struct {
	api *tg.Bot
	cfg *Config
}

func (arch *Archive) UploadTg() error {
	start := time.Now()
	slog.Debug("archive#upload_tg")
	defer slog.Debug("archive#upload_tg_done", "elapsed", time.Since(start))

	posts, err := arch.downloaded.KeysSnapshot()
	if err != nil {
		return fmt.Errorf("tg_archive: upload, keys snapshot, %w", err)
	}

	slices.SortFunc(posts, func(leftId, rightId string) int {
		left, leftErr := arch.downloaded.Get(leftId)
		right, rightErr := arch.downloaded.Get(rightId)
		if leftErr != nil || rightErr != nil {
			return 0
		}
		return cmp.Compare(left.CreateTime, right.CreateTime)
	})

	for _, id := range posts {
		post, err := arch.downloaded.Get(id)
		if err != nil {
			return fmt.Errorf("tg_archive: get post, %w", err)
		}
		if err := arch.uploadPost(post); err != nil {
			arch.newErrorId(post.Id, post.Tag, err)
			//return fmt.Errorf("tg_archive: upload post, %w", err)
		}
	}

	return nil
}

func (arch *Archive) StartBot() {
	arch.tg.
		OnError(tg.OnErrorLog).
		Scheduler().
		Command("/start", tg.Synced(tg.CommonTextReply("hello, this bot archives stuff, source code is available at https://github.com/kittenbark/tg-tiktok-archive"))).
		Command("/info", tg.Synced(arch.tgHandlerInfo)).
		Filter(arch.onAdmin).
		Command("/add", tg.Synced(arch.tgHandlerAdd)).
		Command("/bundle", tg.Synced(arch.tgHandlerBundle)).
		Command("/du", tg.Synced(arch.tgHandlerDu)).
		Start()
}

func (arch *Archive) uploadPost(post *DownloadedPost) error {
	user, err := arch.users.Get(post.Tag)
	if err != nil {
		return err
	}

	errs := []error{}
	for _, target := range user.Tg {
		if target.ThreadId == 0 {
			topic, err := tg.CreateForumTopic(arch.tg.Context(), target.ChatId, post.Tag)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			target.ThreadId = topic.MessageThreadId
			if err := arch.users.Add(post.Tag, user); err != nil {
				errs = append(errs, err)
				continue
			}
		}

		if err := arch.uploadPostTo(post, target.ChatId, target.ThreadId); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := arch.downloaded.Del(post.Id); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) != 0 {
		res := []string{}
		for _, err := range errs {
			res = append(res, err.Error())
		}
		return fmt.Errorf("tg_archive: upload post, [%s]", strings.Join(res, ", "))
	}

	return nil
}

func (arch *Archive) uploadPostTo(post *DownloadedPost, chatId int64, threadId int64) error {
	if post.IsVideo {
		if _, err := tgvideo.Send(arch.tg.Context(), chatId, post.Files[0], &tg.OptSendVideo{MessageThreadId: threadId}); err != nil {
			return err
		}
		return nil
	}

	for _, file := range post.Files {
		_, err := tg.SendDocument(arch.tg.Context(), chatId, tg.FromDisk(file), &tg.OptSendDocument{MessageThreadId: threadId})
		if err != nil {
			return err
		}
	}

	return nil
}

func (arch *Archive) onAdmin(ctx context.Context, upd *tg.Update) bool {
	return tg.OnMessage(ctx, upd) && upd.Message.From != nil && slices.Contains(arch.cfg.Admins, upd.Message.From.Id)
}
