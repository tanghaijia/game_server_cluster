package entity

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSubscriptionIndexPredicate_MatchIsActive 防漂移（M10）：
// 迁移 000027 部分唯一索引的谓词 `status NOT IN (…)` 中的状态集合，
// 必须与 IsActive()==false 的状态集合完全一致。新增状态（如 rebooting）
// 忘记同步迁移时本测试即红。
func TestSubscriptionIndexPredicate_MatchIsActive(t *testing.T) {
	data, err := os.ReadFile("../../migrations/000027_game_instances_subscription_id.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v（防漂移测试依赖该迁移文件）", err)
	}

	// 只匹配 SQL 谓词（注释里的 `status NOT IN (stopped=8, ...)` 含字母，不会误匹配）
	re := regexp.MustCompile(`AND status NOT IN \(([\d,\s]+)\)`)
	m := re.FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("migration missing `status NOT IN (...)` predicate")
	}
	var idxSet []int
	for _, s := range strings.Split(m[1], ",") {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			t.Fatalf("parse index predicate value %q: %v", s, err)
		}
		idxSet = append(idxSet, v)
	}
	sort.Ints(idxSet)

	// 枚举全部状态（0..Failed=10）：IsActive()==false 的集合
	var inactive []int
	for i := 0; i <= int(Failed); i++ {
		if !InstanceStatus(i).IsActive() {
			inactive = append(inactive, i)
		}
	}
	sort.Ints(inactive)

	if len(idxSet) != len(inactive) {
		t.Fatalf("index predicate %v != IsActive-inactive set %v：新增状态未同步迁移 000027？", idxSet, inactive)
	}
	for i := range idxSet {
		if idxSet[i] != inactive[i] {
			t.Fatalf("index predicate %v != IsActive-inactive set %v", idxSet, inactive)
		}
	}
}

func TestIsActive(t *testing.T) {
	active := []InstanceStatus{StatusPending, StatusScheduling, StatusPreparingBuild, StatusRestoringSnapshot, StatusStarting, StatusRunning, StatusStopping, StatusCleaning, StatusQueued}
	inactive := []InstanceStatus{StatusStopped, Failed}

	for _, s := range active {
		if !s.IsActive() {
			t.Fatalf("IsActive(%s) should be true", s)
		}
	}
	for _, s := range inactive {
		if s.IsActive() {
			t.Fatalf("IsActive(%s) should be false", s)
		}
	}
}
