package archive

import (
	"context"
	"fmt"
	"github.com/kittenbark/tg"
	"github.com/kittenbark/tgmedia/tgarchive"
	tikwm "github.com/kittenbark/tikwm/lib"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (arch *Archive) tgHandlerInfo(ctx context.Context, upd *tg.Update) error {
	msg := upd.Message

	args := strings.Split(msg.Text, " ")
	if len(args) != 2 {
		_, err := tg.SendMessage(ctx, msg.Chat.Id, "wrong number of arguments, expected: /info losertron")
		return err
	}

	details, err := tikwm.Details(args[1])
	if err != nil {
		_, err := tg.SendMessage(ctx,
			msg.Chat.Id,
			fmt.Sprintf("couldn't get user details, %s (username: %s)", err.Error(), args[1]),
		)
		return err
	}

	_, err = tg.SendMessage(ctx,
		msg.Chat.Id,
		fmt.Sprintf(">> %s, username: @%s (id: %s)\n"+
			"Followers: %d\n"+
			"Videos: %d\n"+
			"Under 18: %t",
			details.User.Nickname,
			details.User.UniqueId,
			details.User.Id,
			details.Stats.FollowerCount,
			details.Stats.VideoCount,
			details.User.IsUnderAge18,
		),
	)
	return err
}

func (arch *Archive) tgHandlerAdd(ctx context.Context, upd *tg.Update) error {
	msg := upd.Message

	args := strings.Split(msg.Text, " ")
	if len(args) != 3 {
		_, err := tg.SendMessage(ctx, msg.Chat.Id, "wrong number of arguments, expected: /add loser losertron")
		return err
	}

	tag := args[1]
	username := args[2]
	details, err := tikwm.Details(username)
	if err != nil {
		_, err := tg.SendMessage(ctx,
			msg.Chat.Id,
			fmt.Sprintf("couldn't get user details, %s (username: %s)", err.Error(), username),
		)
		return err
	}

	err = arch.users.Add(
		tag,
		&User{
			Username: username,
			Id:       details.User.Id,
			Tg:       []*UserTg{{ChatId: msg.Chat.Id}},
		},
	)
	if err != nil {
		return err
	}

	_, err = tg.SendMessage(ctx,
		msg.Chat.Id,
		fmt.Sprintf("New user %s (%s), username: @%s (id: %s)\n"+
			"Followers: %d\n"+
			"Videos: %d\n"+
			"Under 18: %t",
			tag, details.User.Nickname, details.User.UniqueId, details.User.Id, details.Stats.FollowerCount, details.Stats.VideoCount, details.User.IsUnderAge18,
		),
	)
	return err
}

func (arch *Archive) tgHandlerBundle(ctx context.Context, upd *tg.Update) (err error) {
	msg := upd.Message
	defer func() {
		if err != nil {
			_, _ = tg.SendMessage(ctx, msg.Chat.Id, "sorry, unexpected error occurred")
		}
	}()
	asReply := &tg.ReplyParameters{MessageId: msg.MessageId}

	args := strings.Split(msg.Text, " ")
	if len(args) != 2 {
		_, err := tg.SendMessage(ctx, msg.Chat.Id, "wrong number of arguments, expected: /bundle losertron")
		return err
	}

	tag := args[1]
	_, ok, err := arch.users.TryGet(tag)
	if err != nil {
		return err
	}
	if !ok {
		_, err := tg.SendMessage(ctx, msg.Chat.Id, fmt.Sprintf("user with tag '%s' not found", tag))
		return err
	}

	progress, err := tg.SendMessage(ctx, msg.Chat.Id, "packing..", &tg.OptSendMessage{ReplyParameters: asReply})
	if err != nil {
		return err
	}
	defer func(ctx context.Context, chatId int64, messageId int64) {
		_, _ = tg.DeleteMessage(ctx, chatId, messageId)
	}(ctx, msg.Chat.Id, progress.MessageId)

	_, err = tgarchive.SendBy2GB(
		ctx,
		msg.Chat.Id,
		path.Join(arch.cfg.Data, tag),
		fmt.Sprintf("%s_%s", tag, time.Now().Format(time.DateOnly)),
		&tg.OptSendDocument{ReplyParameters: asReply},
	)

	return err
}

func (arch *Archive) tgHandlerDu(ctx context.Context, upd *tg.Update) error {
	subdirs, err := os.ReadDir(arch.cfg.Data)
	if err != nil {
		return err
	}
	result := []string{}
	for _, subdir := range subdirs {
		if subdir.Name() == ".DS_Store" {
			continue
		}
		result = append(result,
			fmt.Sprintf("%s: %s", subdir.Name(), du(path.Join(arch.cfg.Data, subdir.Name()))),
		)

	}

	msg := upd.Message
	_, err = tg.SendMessage(
		ctx,
		msg.Chat.Id,
		strings.Join(result, "\n"),
		&tg.OptSendMessage{ReplyParameters: &tg.ReplyParameters{MessageId: msg.MessageId}},
	)
	return err
}

func calcDirSize(dir string) (int64, error) {
	size := int64(0)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func du(dir string) string {
	size, err := calcDirSize(dir)
	if err != nil {
		return "<error>"
	}

	const unit = 1024
	div, exp := int64(1), 0
	n := size
	for unit < n {
		div *= unit
		exp++
		n /= unit
	}

	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f%s", float64(size)/float64(div), units[exp])
}
