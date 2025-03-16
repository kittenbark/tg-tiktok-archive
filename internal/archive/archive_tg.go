package archive

import (
	"cmp"
	"fmt"
	"github.com/kittenbark/tg"
	"github.com/kittenbark/tgmedia/tgvideo"
	"slices"
	"strings"
)

type TgArchive struct {
	api *tg.Bot
	cfg *Config
}

// 			wg.Add(1)
//			go func() {
//				defer func() {
//					wg.Done()
//					if err := recover(); err != nil {
//						slog.Error("archive#upload_user_panic", "tag", tag, "err", err)
//					}
//				}()
//				if err := arch.UploadUser(tag, posts); err != nil {
//					slog.Error("archive#upload_user", "tag", tag, "err", err, "posts", posts)
//				}
//			}()
//			slog.Info("archive#user", "tag", tag)
//
//func (arch *TgArchive) UploadUser(tag string, posts [][]string) (err error) {
//	user, err := arch.users.Get(tag)
//	if err != nil {
//		return fmt.Errorf("users: get user: %w", err)
//	}
//
//	for _, userTg := range user.Tg {
//		if userTg.ThreadId == 0 {
//			topic, err := tg.CreateForumTopic(arch.tg.Context(), userTg.ChatId, tag)
//			if err != nil {
//				return fmt.Errorf("tg.CreateForumTopic, %w (%s)", err, tag)
//			}
//			userTg.ThreadId = topic.MessageThreadId
//			if err := arch.users.Add(tag, user); err != nil {
//				return fmt.Errorf("users: add user, %w (%s)", err, tag)
//			}
//		}
//	}
//
//	slices.Reverse(posts)
//
//	for _, post := range posts {
//		wg := &sync.WaitGroup{}
//		for _, info := range user.Tg {
//			wg.Add(1)
//			go func() {
//				for _, media := range post {
//					var sendErr error
//					if strings.HasSuffix(media, ".mp4") {
//						_, sendErr = tgvideo.Send(arch.tg.Context(), info.ChatId, media, &tg.OptSendVideo{MessageThreadId: info.ThreadId})
//					} else {
//						_, sendErr = tg.SendDocument(arch.tg.Context(), info.ChatId, tg.FromDisk(media), &tg.OptSendDocument{MessageThreadId: info.ThreadId})
//					}
//					if sendErr != nil {
//						err = sendErr
//					}
//				}
//
//			}()
//		}
//		wg.Wait()
//	}
//
//	return nil
//}

func (arch *Archive) UploadTg() error {
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
			return fmt.Errorf("tg_archive: upload post, %w", err)
		}
	}

	return nil
}

func (arch *Archive) StartBot() {
	arch.tg.
		OnError(tg.OnErrorLog).
		Scheduler().
		Command("/info", tg.Synced(arch.tgHandlerInfo)).
		Filter(arch.onAdmin).
		Command("/add", tg.Synced(arch.tgHandlerAdd)).
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
