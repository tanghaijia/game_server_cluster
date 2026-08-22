#!/usr/bin/env bats
# ============================================================
# lib/port-inject.sh 契约测试（XML 属性改写）
# ============================================================
setup() {
  TEST_DIR="$(mktemp -d)"
  export TEST_DIR
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/log.sh"
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/fs.sh"
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/port-inject.sh"
}

teardown() {
  rm -rf "${TEST_DIR}"
}

@test "inject_xml_property: 替换已有属性值" {
  cat > "${TEST_DIR}/s.xml" <<'EOF'
<?xml version="1.0"?>
<ServerSettings>
	<property name="ServerPort" value="26900"/>
	<property name="ServerName" value="My Game Host"/>
</ServerSettings>
EOF
  inject_xml_property "${TEST_DIR}/s.xml" "ServerPort" "30000"
  grep -q '<property name="ServerPort" value="30000"/>' "${TEST_DIR}/s.xml"
  # 其他属性不受影响
  grep -q '<property name="ServerName" value="My Game Host"/>' "${TEST_DIR}/s.xml"
}

@test "inject_xml_property: 属性不存在时插入" {
  cat > "${TEST_DIR}/s.xml" <<'EOF'
<?xml version="1.0"?>
<ServerSettings>
	<property name="ServerName" value="My Game Host"/>
</ServerSettings>
EOF
  inject_xml_property "${TEST_DIR}/s.xml" "ServerPort" "30000"
  grep -q '<property name="ServerPort" value="30000"/>' "${TEST_DIR}/s.xml"
}

@test "inject_xml_property: 文件不存在返回 1" {
  run inject_xml_property "${TEST_DIR}/missing.xml" "ServerPort" "30000"
  [ "$status" -eq 1 ]
}

@test "inject_xml_property: 无 ServerSettings 根节点返回 1 且不破坏文件" {
  echo "not xml" > "${TEST_DIR}/bad.xml"
  run inject_xml_property "${TEST_DIR}/bad.xml" "ServerPort" "30000"
  [ "$status" -eq 1 ]
  [ "$(cat "${TEST_DIR}/bad.xml")" = "not xml" ]
}

@test "apply_port_inject: GAME_HOST_PORT 为空时跳过" {
  GAME_HOST_PORT=""
  run apply_port_inject xml_property "${TEST_DIR}/s.xml" ServerPort
  [ "$status" -eq 0 ]
}

@test "apply_port_inject: 端口注入生效" {
  GAME_HOST_PORT="31000"
  cat > "${TEST_DIR}/s.xml" <<'EOF'
<?xml version="1.0"?>
<ServerSettings>
	<property name="ServerPort" value="26900"/>
</ServerSettings>
EOF
  apply_port_inject xml_property "${TEST_DIR}/s.xml" ServerPort
  grep -q '<property name="ServerPort" value="31000"/>' "${TEST_DIR}/s.xml"
}
