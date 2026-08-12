package op

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// userCache 被登录路径（UserVerify / UserGet）读、被改密改名路径写，必须加锁。
// 另外所有写路径都是「先写库成功再更新缓存」——反过来会在 DB 失败时把内存和
// 库改到不一致，例如改密失败后旧密码已经在内存里失效。
var (
	userCacheLock sync.RWMutex
	// userWriteLock serializes the complete DB-write/cache-commit sequence.
	// userCacheLock alone only protects individual memory accesses and cannot
	// prevent two writers from committing DB and cache updates in different orders.
	userWriteLock sync.Mutex
	userCache     model.User
)

func UserInit() error {
	userWriteLock.Lock()
	defer userWriteLock.Unlock()

	userCacheLock.Lock()
	defer userCacheLock.Unlock()

	if err := db.GetDB().First(&userCache).Error; err == nil {
		return nil
	}

	initial := model.User{Username: "admin", Password: "admin"}
	if err := initial.HashPassword(); err != nil {
		return err
	}
	if err := db.GetDB().Create(&initial).Error; err != nil {
		return err
	}
	userCache = initial
	log.Infof("initial user: admin,password: admin")
	return nil
}

func UserChangePassword(oldPassword, newPassword string) error {
	userWriteLock.Lock()
	defer userWriteLock.Unlock()

	current := userSnapshot()

	if err := current.ComparePassword(oldPassword); err != nil {
		return apperror.Wrap(apperror.CodeAuthPasswordIncorrect, "incorrect old password", err).WithStatus(http.StatusBadRequest)
	}

	updated := current
	updated.Password = newPassword
	if err := updated.HashPassword(); err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := db.GetDB().Model(&model.User{}).Where("id = ?", current.ID).
		Update("password", updated.Password).Error; err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	userCacheLock.Lock()
	userCache.Password = updated.Password
	userCacheLock.Unlock()

	return nil
}

func UserChangeUsername(newUsername string) error {
	userWriteLock.Lock()
	defer userWriteLock.Unlock()

	current := userSnapshot()
	if current.Username == newUsername {
		return fmt.Errorf("new username is the same as the old username")
	}

	if err := db.GetDB().Model(&model.User{}).Where("id = ?", current.ID).
		Update("username", newUsername).Error; err != nil {
		return fmt.Errorf("failed to update username: %w", err)
	}

	userCacheLock.Lock()
	userCache.Username = newUsername
	userCacheLock.Unlock()

	return nil
}

func UserVerify(username, password string) error {
	current := userSnapshot()
	if username != current.Username {
		return fmt.Errorf("incorrect username")
	}
	if err := current.ComparePassword(password); err != nil {
		return fmt.Errorf("incorrect password")
	}
	return nil
}

func UserGet() model.User {
	return userSnapshot()
}

// userSnapshot 返回 userCache 的值拷贝，调用方拿到后可以在锁外自由使用。
func userSnapshot() model.User {
	userCacheLock.RLock()
	defer userCacheLock.RUnlock()
	return userCache
}
