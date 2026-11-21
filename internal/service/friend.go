package service

import (
	"context"
	"fmt"

	"github.com/dloshkarev/highloadarchitect/internal/repository"
)

type FriendService struct {
	friends repository.FriendRepository
}

func NewFriendService(friends repository.FriendRepository) *FriendService {
	return &FriendService{friends: friends}
}

// Set добавляет пользователя friendID в друзья userID. Повторный вызов идемпотентен.
func (s *FriendService) Set(ctx context.Context, userID string, friendID string) error {
	if err := validateUserID(friendID); err != nil {
		return err
	}

	if userID == friendID {
		return validationError("нельзя добавить в друзья самого себя")
	}

	if err := s.friends.Add(ctx, userID, friendID); err != nil {
		return fmt.Errorf("add friend: %w", err)
	}

	return nil
}

// Delete удаляет пользователя friendID из друзей userID. Повторный вызов идемпотентен.
func (s *FriendService) Delete(ctx context.Context, userID string, friendID string) error {
	if err := validateUserID(friendID); err != nil {
		return err
	}

	if err := s.friends.Delete(ctx, userID, friendID); err != nil {
		return fmt.Errorf("delete friend: %w", err)
	}

	return nil
}
