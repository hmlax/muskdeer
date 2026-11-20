package service

import (
	"google.golang.org/protobuf/proto"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// filterRoleMenuTree keeps granted pages and their ancestor containers from the
// active menu tree. A container never grants access to its unselected children.
func filterRoleMenuTree(items []*operatorV1.Menu, granted map[uint32]bool) []*operatorV1.Menu {
	filtered := make([]*operatorV1.Menu, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		children := filterRoleMenuTree(item.Children, granted)
		catalog := item.GetType() == operatorV1.Menu_CATALOG || item.GetComponent() == "BasicLayout"
		if catalog && len(children) == 0 {
			continue
		}
		if !granted[item.GetId()] && !catalog {
			continue
		}
		result := proto.Clone(item).(*operatorV1.Menu)
		result.Children = children
		if len(children) != len(item.Children) {
			result.Redirect = nil
		}
		filtered = append(filtered, result)
	}
	return filtered
}
