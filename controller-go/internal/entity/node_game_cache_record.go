package entity

import "time"

type NodeGameCacheRecord struct {
	id               string
	nodeAgentId      string
	gameId           string
	branchName       string
	buildId          string
	status           NodeGameCacheRecordStatus
	path             string
	downloadProgress float32
	createTime       time.Time
	updateTime       time.Time
}

type NodeGameCacheRecordStatus int

const (
	DOWNLOADING NodeGameCacheRecordStatus = iota
	AVAILABLE
	REMOVED
	UNAVAILABLE
)
