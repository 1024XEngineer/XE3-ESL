package presentation

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Repository interface {
	Catalog(context.Context) (Catalog, error)
	FindPreference(context.Context, string) (Preference, error)
	SavePreference(context.Context, Preference, int64) (Preference, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrRepository
	}
	return &Service{repository: repository}, nil
}

func (service *Service) GetCatalog(
	ctx context.Context,
	actor requestcontext.Actor,
) (Catalog, error) {
	if service == nil || service.repository == nil || !actor.Valid() {
		return Catalog{}, ErrInvalidRequest
	}
	catalog, err := service.repository.Catalog(ctx)
	if err != nil {
		return Catalog{}, err
	}
	if !catalog.Valid() {
		return Catalog{}, ErrRepository
	}
	return cloneCatalog(catalog), nil
}

func (service *Service) GetPreference(
	ctx context.Context,
	actor requestcontext.Actor,
) (Preference, error) {
	if service == nil || service.repository == nil || !actor.Valid() {
		return Preference{}, ErrInvalidRequest
	}
	preference, err := service.repository.FindPreference(ctx, actor.UserID)
	if errors.Is(err, ErrNotFound) {
		catalog, catalogErr := service.GetCatalog(ctx, actor)
		if catalogErr != nil {
			return Preference{}, catalogErr
		}
		return DefaultPreference(actor.UserID, catalog), nil
	}
	if err != nil {
		return Preference{}, err
	}
	if !preference.ValidLogical() || preference.Version == 0 ||
		preference.UserID != actor.UserID {
		return Preference{}, ErrRepository
	}
	return preference, nil
}

func (service *Service) UpdatePreference(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateCommand,
) (Preference, error) {
	if service == nil || service.repository == nil || !actor.Valid() ||
		!command.Valid() {
		return Preference{}, ErrInvalidRequest
	}
	catalog, err := service.GetCatalog(ctx, actor)
	if err != nil {
		return Preference{}, err
	}
	if !catalog.Contains(command.AvatarOptionID, command.VoiceOptionID) {
		return Preference{}, ErrInvalidRequest
	}
	current, err := service.GetPreference(ctx, actor)
	if err != nil {
		return Preference{}, err
	}
	if current.Version != command.ExpectedVersion {
		return Preference{}, ErrVersionConflict
	}
	next := Preference{
		UserID:         actor.UserID,
		AvatarOptionID: command.AvatarOptionID,
		VoiceOptionID:  command.VoiceOptionID,
		Version:        current.Version + 1,
	}
	saved, err := service.repository.SavePreference(ctx, next, current.Version)
	if err != nil {
		return Preference{}, err
	}
	if !saved.ValidForPersistence() || saved.UserID != actor.UserID ||
		saved.Version != next.Version {
		return Preference{}, ErrRepository
	}
	return saved, nil
}

func cloneCatalog(catalog Catalog) Catalog {
	catalog.Avatars = append([]AvatarOption(nil), catalog.Avatars...)
	catalog.Voices = append([]VoiceOption(nil), catalog.Voices...)
	return catalog
}
