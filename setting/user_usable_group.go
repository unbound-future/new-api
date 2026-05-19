package setting

import (
	"encoding/json"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var userUsableGroups = map[string]string{
	"default": "默认分组",
	"vip":     "vip分组",
}
var userUsableGroupsMutex sync.RWMutex

// groupDescriptions 保存所有分组的描述（包括未勾选「用户可选」的分组），
// 仅用于管理后台展示与白名单规则的默认描述查询，不影响用户可选列表。
var groupDescriptions = map[string]string{}
var groupDescriptionsMutex sync.RWMutex

func GetUserUsableGroupsCopy() map[string]string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	copyUserUsableGroups := make(map[string]string)
	for k, v := range userUsableGroups {
		copyUserUsableGroups[k] = v
	}
	return copyUserUsableGroups
}

func UserUsableGroups2JSONString() string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	jsonBytes, err := json.Marshal(userUsableGroups)
	if err != nil {
		common.SysLog("error marshalling user groups: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateUserUsableGroupsByJSONString(jsonStr string) error {
	userUsableGroupsMutex.Lock()
	defer userUsableGroupsMutex.Unlock()

	userUsableGroups = make(map[string]string)
	return json.Unmarshal([]byte(jsonStr), &userUsableGroups)
}

func GetUsableGroupDescription(groupName string) string {
	userUsableGroupsMutex.RLock()
	if desc, ok := userUsableGroups[groupName]; ok {
		userUsableGroupsMutex.RUnlock()
		return desc
	}
	userUsableGroupsMutex.RUnlock()

	groupDescriptionsMutex.RLock()
	defer groupDescriptionsMutex.RUnlock()
	if desc, ok := groupDescriptions[groupName]; ok {
		return desc
	}
	return groupName
}

func GroupDescriptions2JSONString() string {
	groupDescriptionsMutex.RLock()
	defer groupDescriptionsMutex.RUnlock()

	jsonBytes, err := json.Marshal(groupDescriptions)
	if err != nil {
		common.SysLog("error marshalling group descriptions: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateGroupDescriptionsByJSONString(jsonStr string) error {
	groupDescriptionsMutex.Lock()
	defer groupDescriptionsMutex.Unlock()

	groupDescriptions = make(map[string]string)
	if jsonStr == "" {
		return nil
	}
	return json.Unmarshal([]byte(jsonStr), &groupDescriptions)
}
