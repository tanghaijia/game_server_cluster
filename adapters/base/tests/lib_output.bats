#!/usr/bin/env bats
# ============================================================
# lib/output.sh 契约测试（平台 JSON 契约）
# ============================================================
setup() {
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/output.sh"
}

@test "emit_health: healthy=true 输出合法 JSON" {
  run emit_health true
  [ "$status" -eq 0 ]
  [ "$output" = '{"healthy": true, "reason": "ok"}' ]
}

@test "emit_health: healthy=false 带自定义 reason" {
  run emit_health false "process not running"
  [ "$output" = '{"healthy": false, "reason": "process not running"}' ]
}

@test "emit_health: 输出可被 jq 解析且字段正确" {
  run emit_health true "running"
  [ "$(echo "$output" | jq -r '.healthy')" = "true" ]
  [ "$(echo "$output" | jq -r '.reason')" = "running" ]
}

@test "emit_players: 默认 0" {
  run emit_players
  [ "$output" = '{"players": 0}' ]
}

@test "emit_players: 带 max_players" {
  run emit_players 3 32
  [ "$output" = '{"players": 3, "max_players": 32}' ]
}

@test "health_by_process_name: 无匹配进程时 healthy=false" {
  run health_by_process_name "__definitely_not_running_proc__"
  [ "$(echo "$output" | jq -r '.healthy')" = "false" ]
}
