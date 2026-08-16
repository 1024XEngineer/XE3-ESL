package profile

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Repository interface {
	Find(context.Context, string) (Profile, error)
	Save(context.Context, Profile, int64) (Profile, error)
}

type DataPatch struct {
	FormOfAddress       *string
	Occupation          *string
	ProfessionalContext *string
	NativeLanguage      *string
	ExplanationLanguage *string
	ResponseDetail      *ResponseDetail
	Interests           *[]string
}

func (patch DataPatch) Fields() []Field {
	result := make([]Field, 0, len(fields))
	if patch.FormOfAddress != nil {
		result = append(result, FieldFormOfAddress)
	}
	if patch.Occupation != nil {
		result = append(result, FieldOccupation)
	}
	if patch.ProfessionalContext != nil {
		result = append(result, FieldProfessionalContext)
	}
	if patch.NativeLanguage != nil {
		result = append(result, FieldNativeLanguage)
	}
	if patch.ExplanationLanguage != nil {
		result = append(result, FieldExplanationLanguage)
	}
	if patch.ResponseDetail != nil {
		result = append(result, FieldResponseDetail)
	}
	if patch.Interests != nil {
		result = append(result, FieldInterests)
	}
	return result
}

func (patch DataPatch) Apply(data Data) (Data, bool) {
	if patch.FormOfAddress != nil {
		data.FormOfAddress = *patch.FormOfAddress
	}
	if patch.Occupation != nil {
		data.Occupation = *patch.Occupation
	}
	if patch.ProfessionalContext != nil {
		data.ProfessionalContext = *patch.ProfessionalContext
	}
	if patch.NativeLanguage != nil {
		data.NativeLanguage = *patch.NativeLanguage
	}
	if patch.ExplanationLanguage != nil {
		data.ExplanationLanguage = *patch.ExplanationLanguage
	}
	if patch.ResponseDetail != nil {
		data.ResponseDetail = *patch.ResponseDetail
	}
	if patch.Interests != nil {
		data.Interests = append([]string(nil), (*patch.Interests)...)
	}
	return data, data.Valid()
}

type UpdateCommand struct {
	ExpectedVersion int64
	Patch           DataPatch
	ForgetFields    []Field
	ClearProfile    bool
	MemoryEnabled   *bool
	SourceType      SourceType
	SourceMessageID string
}

func (command UpdateCommand) Valid() bool {
	patched := command.Patch.Fields()
	if command.ExpectedVersion < 0 ||
		len(patched)+len(command.ForgetFields) == 0 &&
			!command.ClearProfile && command.MemoryEnabled == nil ||
		command.ClearProfile && (len(patched) > 0 || len(command.ForgetFields) > 0) ||
		len(command.ForgetFields) > len(fields) {
		return false
	}
	seen := make(map[Field]struct{}, len(patched)+len(command.ForgetFields))
	for _, field := range patched {
		seen[field] = struct{}{}
	}
	for _, field := range command.ForgetFields {
		if !field.Valid() {
			return false
		}
		if _, duplicate := seen[field]; duplicate {
			return false
		}
		seen[field] = struct{}{}
	}
	if len(patched) == 0 {
		return command.SourceType == SourceUserSetting && command.SourceMessageID == ""
	}
	if !command.SourceType.Valid() {
		return false
	}
	if command.SourceType == SourceUserSetting {
		return command.SourceMessageID == ""
	}
	return validIdentifier(command.SourceMessageID)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository, now func() time.Time) (*Service, error) {
	if repository == nil || now == nil {
		return nil, ErrRepository
	}
	return &Service{repository: repository, now: now}, nil
}

func (service *Service) Get(
	ctx context.Context,
	actor requestcontext.Actor,
) (Profile, error) {
	if service == nil || service.repository == nil || !actor.Valid() {
		return Profile{}, ErrInvalidRequest
	}
	item, err := service.repository.Find(ctx, actor.UserID)
	if errors.Is(err, ErrNotFound) {
		return Empty(actor.UserID), nil
	}
	if err != nil {
		return Profile{}, err
	}
	if !item.ValidStored() || item.UserID != actor.UserID {
		return Profile{}, ErrRepository
	}
	return cloneProfile(item), nil
}

func (service *Service) Update(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateCommand,
) (Profile, error) {
	if service == nil || service.repository == nil || !actor.Valid() ||
		!command.Valid() {
		return Profile{}, ErrInvalidRequest
	}
	current, err := service.Get(ctx, actor)
	if err != nil {
		return Profile{}, err
	}
	if current.Version != command.ExpectedVersion {
		return Profile{}, ErrVersionConflict
	}
	next := cloneProfile(current)
	if command.ClearProfile {
		next.Data = Data{}
		next.FieldSources = map[Field]FieldSource{}
	} else {
		var valid bool
		next.Data, valid = command.Patch.Apply(next.Data)
		if !valid {
			return Profile{}, ErrInvalidRequest
		}
		for _, field := range command.ForgetFields {
			clearField(&next.Data, field)
			delete(next.FieldSources, field)
		}
		if len(command.Patch.Fields()) > 0 {
			source := FieldSource{
				Type:       command.SourceType,
				MessageID:  command.SourceMessageID,
				RecordedAt: service.now().UTC(),
			}
			if !source.Valid() {
				return Profile{}, ErrInvalidRequest
			}
			for _, field := range command.Patch.Fields() {
				next.FieldSources[field] = source
			}
		}
	}
	if command.MemoryEnabled != nil {
		next.MemoryEnabled = *command.MemoryEnabled
	}
	next.Version = current.Version + 1
	if !next.ValidForPersistence() {
		return Profile{}, ErrInvalidRequest
	}
	saved, err := service.repository.Save(ctx, next, current.Version)
	if err != nil {
		return Profile{}, err
	}
	if !saved.ValidStored() || saved.UserID != actor.UserID ||
		saved.Version != next.Version {
		return Profile{}, ErrRepository
	}
	return cloneProfile(saved), nil
}

func (profile Profile) ValidForPersistence() bool {
	return validIdentifier(profile.UserID) && profile.Data.Valid() &&
		profile.Version > 0 && validSources(profile.Data, profile.FieldSources)
}

func clearField(data *Data, field Field) {
	switch field {
	case FieldFormOfAddress:
		data.FormOfAddress = ""
	case FieldOccupation:
		data.Occupation = ""
	case FieldProfessionalContext:
		data.ProfessionalContext = ""
	case FieldNativeLanguage:
		data.NativeLanguage = ""
	case FieldExplanationLanguage:
		data.ExplanationLanguage = ""
	case FieldResponseDetail:
		data.ResponseDetail = ""
	case FieldInterests:
		data.Interests = nil
	}
}

func cloneProfile(profile Profile) Profile {
	profile.Data.Interests = slices.Clone(profile.Data.Interests)
	sources := profile.FieldSources
	profile.FieldSources = make(map[Field]FieldSource, len(sources))
	for field, source := range sources {
		profile.FieldSources[field] = source
	}
	return profile
}
