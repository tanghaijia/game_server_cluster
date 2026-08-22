package biz

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ============================================================
// adapter_schema.go —— 适配器配置 schema 的 Go 侧解析与校验
//
// 与 asset_service 的 AdapterSchema（Rust）保持字段契约一致；
// schema_json 由 adapters/tools/gen_manifest.py 生成、随 GameBuild 注册。
// 用途：
//   1. 创建实例时校验用户提交的 config（未知 key / locked / 类型 / 范围 / 枚举）
//   2. 暴露给前端生成配置表单（player 项）
// ============================================================

// AdapterConfigSetting 单条配置声明（adapter.toml [config."<key>"] 段）
type AdapterConfigSetting struct {
	Key            string   `json:"key"`
	Type           string   `json:"type"`    // string / int / bool / enum
	Control        string   `json:"control"` // player / platform / locked
	Apply          string   `json:"apply"`   // always / on_first_start
	Render         string   `json:"render"`  // xml_property / envsubst / sed_pattern
	Default        *string  `json:"default"`
	Min            *int64   `json:"min"`
	Max            *int64   `json:"max"`
	EnumValues     []string `json:"enum_values"`
	Secret         bool     `json:"secret"`
	LabelKey       *string  `json:"label_key"`
	DescriptionKey *string  `json:"description_key"`
	GroupKey       *string  `json:"group_key"`
}

// AdapterSchema 完整配置 schema（含 i18n 字典）
type AdapterSchema struct {
	AdapterID  string                 `json:"adapter_id"`
	GameID     string                 `json:"game_id"`
	Settings   []AdapterConfigSetting `json:"settings"`
	RenderFile *string                `json:"render_file"`
	I18n       map[string]any         `json:"i18n"`
}

// ParseAdapterSchema 解析 schema_json（非法 JSON / 结构错误返回 error）
func ParseAdapterSchema(schemaJSON string) (*AdapterSchema, error) {
	var s AdapterSchema
	if err := json.Unmarshal([]byte(schemaJSON), &s); err != nil {
		return nil, fmt.Errorf("schema_json 解析失败: %w", err)
	}
	if s.AdapterID == "" {
		return nil, fmt.Errorf("schema.adapter_id 为空")
	}
	return &s, nil
}

// ValidateInstanceConfig 校验实例配置键值对（创建实例时调用）：
// 未知 key 拒绝；locked 项拒绝；int 范围/bool/enum 类型校验。
func ValidateInstanceConfig(schema *AdapterSchema, config map[string]string) error {
	byKey := make(map[string]*AdapterConfigSetting, len(schema.Settings))
	for i := range schema.Settings {
		byKey[schema.Settings[i].Key] = &schema.Settings[i]
	}

	for key, value := range config {
		setting, ok := byKey[key]
		if !ok {
			return fmt.Errorf("未知配置项: %s", key)
		}
		if setting.Control == "locked" {
			return fmt.Errorf("配置项 %s 由平台锁定，不可配置", key)
		}
		switch setting.Type {
		case "int":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("配置项 %s 需要整数，收到: %s", key, value)
			}
			if setting.Min != nil && n < *setting.Min {
				return fmt.Errorf("配置项 %s 小于最小值 %d", key, *setting.Min)
			}
			if setting.Max != nil && n > *setting.Max {
				return fmt.Errorf("配置项 %s 大于最大值 %d", key, *setting.Max)
			}
		case "bool":
			if value != "true" && value != "false" {
				return fmt.Errorf("配置项 %s 需要 true/false，收到: %s", key, value)
			}
		case "enum":
			valid := false
			for _, v := range setting.EnumValues {
				if v == value {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("配置项 %s 值非法: %s（可选: %v）", key, value, setting.EnumValues)
			}
		}
	}
	return nil
}

// PlayerConfigSettings 返回玩家可见配置项（control=player），供前端表单生成
func (s *AdapterSchema) PlayerConfigSettings() []AdapterConfigSetting {
	out := make([]AdapterConfigSetting, 0, len(s.Settings))
	for _, st := range s.Settings {
		if st.Control == "player" {
			out = append(out, st)
		}
	}
	return out
}
