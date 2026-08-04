// 本文件实现 GORM Record 与 Resume 领域模型之间的转换边界。
package persistence

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// resumeToRecord 把 Resume 领域模型转换为持久化 Record。
func resumeToRecord(item resume.Resume) resumeRecord {
	record := resumeRecord{
		ResumeID:         item.ID,
		OwnerUserID:      item.OwnerUserID,
		Title:            item.Title,
		OriginalFilename: item.OriginalFilename,
		ContentType:      item.ContentType,
		SizeBytes:        item.SizeBytes,
		ChecksumSHA256:   item.ChecksumSHA256,
		ObjectKey:        item.ObjectKey,
		FileStatus:       string(item.FileStatus),
		ParseStatus:      string(item.ParseStatus),
		Version:          item.Version,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
	if item.ParseFailureCode != "" {
		record.ParseFailureCode = stringPointer(item.ParseFailureCode)
	}
	if item.CurrentRevision > 0 {
		record.CurrentRevision = int64Pointer(item.CurrentRevision)
	}
	if item.DeletedAt != nil {
		record.DeletedAt.Time = item.DeletedAt.UTC()
		record.DeletedAt.Valid = true
	}
	return record
}

// resumeFromRecord 把持久化 Record 转换为 Resume 领域模型。
func resumeFromRecord(record resumeRecord) (resume.Resume, error) {
	if record.ResumeID == "" || record.OwnerUserID == "" ||
		record.Title == "" || record.OriginalFilename == "" ||
		record.ContentType != "application/pdf" || record.SizeBytes < 1 ||
		record.ChecksumSHA256 == "" || record.ObjectKey == "" ||
		record.Version < 1 || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return resume.Resume{}, errors.New("resume: invalid persisted resume")
	}
	fileStatus := resume.FileStatus(record.FileStatus)
	parseStatus := resume.ParseStatus(record.ParseStatus)
	if !validFileStatus(fileStatus) || !validParseStatus(parseStatus) {
		return resume.Resume{}, errors.New("resume: invalid persisted status")
	}
	item := resume.Resume{
		ID:               record.ResumeID,
		OwnerUserID:      record.OwnerUserID,
		Title:            record.Title,
		OriginalFilename: record.OriginalFilename,
		ContentType:      record.ContentType,
		SizeBytes:        record.SizeBytes,
		ChecksumSHA256:   record.ChecksumSHA256,
		ObjectKey:        record.ObjectKey,
		FileStatus:       fileStatus,
		ParseStatus:      parseStatus,
		Version:          record.Version,
		CreatedAt:        record.CreatedAt.UTC(),
		UpdatedAt:        record.UpdatedAt.UTC(),
	}
	if record.ParseFailureCode != nil {
		item.ParseFailureCode = *record.ParseFailureCode
	}
	if record.CurrentRevision != nil {
		item.CurrentRevision = *record.CurrentRevision
	}
	if record.DeletedAt.Valid {
		deletedAt := record.DeletedAt.Time.UTC()
		item.DeletedAt = &deletedAt
	}
	return item, nil
}

// revisionToRecord 把结构化内容修订转换为持久化 Record。
func revisionToRecord(item resume.Revision) (revisionRecord, error) {
	if item.ResumeID == "" || item.Revision < 1 || !validRevisionSource(item.Source) {
		return revisionRecord{}, errors.New("resume: invalid revision")
	}
	if (item.Source == resume.RevisionParser && strings.TrimSpace(item.ParserVersion) == "") ||
		(item.Source == resume.RevisionManual && item.ParserVersion != "") {
		return revisionRecord{}, errors.New("resume: invalid revision parser version")
	}
	content, err := json.Marshal(item.Content)
	if err != nil {
		return revisionRecord{}, errors.New("resume: encode revision content")
	}
	record := revisionRecord{
		ResumeID:  item.ResumeID,
		Revision:  item.Revision,
		Source:    string(item.Source),
		Content:   content,
		CreatedAt: item.CreatedAt,
	}
	if item.ParserVersion != "" {
		record.ParserVersion = stringPointer(item.ParserVersion)
	}
	return record, nil
}

// revisionFromRecord 把持久化 Record 转换为结构化内容修订。
func revisionFromRecord(record revisionRecord) (resume.Revision, error) {
	source := resume.RevisionSource(record.Source)
	if record.ResumeID == "" || record.Revision < 1 ||
		!validRevisionSource(source) || len(record.Content) == 0 {
		return resume.Revision{}, errors.New("resume: invalid persisted revision")
	}
	var content resume.Content
	if err := json.Unmarshal(record.Content, &content); err != nil {
		return resume.Revision{}, errors.New("resume: decode revision content")
	}
	item := resume.Revision{
		ResumeID:  record.ResumeID,
		Revision:  record.Revision,
		Source:    source,
		Content:   content,
		CreatedAt: record.CreatedAt.UTC(),
	}
	if record.ParserVersion != nil {
		item.ParserVersion = *record.ParserVersion
	}
	return item, nil
}

// validFileStatus 判断持久化文件状态是否属于领域允许集合。
func validFileStatus(status resume.FileStatus) bool {
	return status == resume.FileUploading || status == resume.FileAvailable ||
		status == resume.FileDeleting || status == resume.FileDeleted
}

// validParseStatus 判断持久化解析状态是否属于领域允许集合。
func validParseStatus(status resume.ParseStatus) bool {
	return status == resume.ParseQueued || status == resume.ParseRunning ||
		status == resume.ParseReady || status == resume.ParseFailed
}

// validRevisionSource 判断持久化修订来源是否属于领域允许集合。
func validRevisionSource(source resume.RevisionSource) bool {
	return source == resume.RevisionParser || source == resume.RevisionManual
}

// int64Pointer 返回 int64 值的独立指针。
func int64Pointer(value int64) *int64 {
	return &value
}

// stringPointer 返回字符串值的独立指针。
func stringPointer(value string) *string {
	return &value
}
