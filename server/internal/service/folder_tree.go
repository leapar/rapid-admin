// 文件夹树 —— 多级文件夹(M125)的公共计算
//
// 设计见 docs/文件系统模块设计方案V1.0.md §2.7(层级从"扁平"改成"多级")
//
// ⚠️ **为什么单独成文件**:路径、子级数、祖先链、子树这四件事都依赖**同一份
// 骨架**(id / name / parent_id)。各写各的会出现"面包屑算出来的路径和列表里
// 显示的不一样" —— 而这种不一致**不会报错**,只表现为界面上两条路径对不上,
// 排障时极难想到是两份实现分叉了。
//
// ⚠️ **骨架是"某个用户的全部文件夹",不是"当前页的文件夹"** —— 祖先可能不在
// 当前页里(分页!),所以算路径必须看全量。
package service

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// maxFolderDepth 文件夹最多几层(根级算第 1 层)
//
// ⚠️ **不是为了好看,是为了兜住两件事**:
//  1. 路径字符串长度(每层最多 128 字节,10 层就是 1.2KB ——
//     面包屑一行放不下,而且它会出现在每个下拉框的 label 里);
//  2. 递归删除的深度(删一个根文件夹要遍历整棵子树)。
//
// ⚠️ **别把它设成"不限制"**:将来真要放开,先想清楚"一个用户能造出多深的树"
// 这件事对上面两条的影响,而不是直接删掉这个常量。
const maxFolderDepth = 10

// folderTree 某个用户的文件夹骨架。
type folderTree struct {
	// nodes id → 文件夹行
	nodes map[uint64]model.SysFolder
	// children 父 id → 子 id 列表(**按 id 升序**)
	//
	// ⚠️ 根级文件夹的父 id 是 0,所以 children[0] 就是"全部根级文件夹"。
	children map[uint64][]uint64
}

// loadFolderTree 读出某用户的文件夹骨架。
//
// ⚠️ **biz 为空 = 跨业务段全量读**。调用方按需要传:
//
//	· 列文件夹(List)传查询里那个 biz —— 保持口径一致;
//	· 算祖先链 / 子树(Detail / Delete)传空 —— 祖先和子孙**必须**全都在,
//	  少一层就会出现"面包屑少一级"或"子文件夹没被一起删掉"。
//
// ⚠️ **只取 status = normal 的行**。软删掉的文件夹不该出现在路径里,
// 也不该被当成还活着的子级 —— 否则用户删了"图纸"之后,路径里还挂着它。
func loadFolderTree(ctx context.Context, db *gorm.DB, userID uint64, biz string) (*folderTree, error) {
	var rows []model.SysFolder
	tx := db.WithContext(ctx).Model(&model.SysFolder{}).
		Where("user_id = ? AND status = ?", userID, model.FileStatusNormal)
	if biz != "" {
		tx = tx.Where("biz = ?", biz)
	}
	if err := tx.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件夹失败").WithDetail(err.Error())
	}
	t := &folderTree{
		nodes:    make(map[uint64]model.SysFolder, len(rows)),
		children: make(map[uint64][]uint64),
	}
	for _, r := range rows {
		t.nodes[r.ID] = r
		t.children[r.ParentID] = append(t.children[r.ParentID], r.ID)
	}
	// 子级顺序显式排一次。上面已经 Order("id ASC"),追加出来天然有序,
	// 但**依赖查询顺序**是隐式约定:哪天有人改了 Order,这里会跟着变,而且
	// 界面上的目录顺序会跟着抖。
	for k := range t.children {
		ids := t.children[k]
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		t.children[k] = ids
	}
	return t, nil
}

// ancestors 从根到直接上级(不含自己)。
//
// ⚠️ **遇到环会停**。parent_id 是应用层写的,理论上不会成环(建子文件夹时
// 父必须是已存在的行),但"库里有一行脏数据"这种事是拦不住的 ——
// 而一个成环的树会让"往上找祖先"**死循环**,把整个列表页挂住。
// 所以这里带 visited 集合,见到重复的 id 就停。
//
// ⚠️ **遇到"上级不在骨架里"(被软删 / 不在同一 biz)也停** —— 那时的正确
// 表现是"路径到这一层为止",而不是编一个不存在的上级出来。
func (t *folderTree) ancestors(id uint64) []model.SysFolder {
	var chain []model.SysFolder
	seen := map[uint64]bool{}
	cur := id
	for {
		n, ok := t.nodes[cur]
		if !ok || seen[cur] {
			break
		}
		seen[cur] = true
		if cur != id {
			chain = append(chain, n)
		}
		if n.ParentID == 0 {
			break
		}
		cur = n.ParentID
	}
	// chain 是"直接上级 → 根",反转成"根 → 直接上级"(面包屑要这个顺序)
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// path 这个文件夹的**完整路径**,形如 "素材/图纸"(**含自身**)。
//
// ⚠️ **含自身** —— 因为它最大的用途是**下拉框的标签**:多级之后同一个用户下
//
//	可以有 "A/图纸" 和 "B/图纸",只显示 "图纸" 分不清是哪个。
//	前端拿它当 label 直接用,不用自己拼 `path + '/' + name`。
//
// ⚠️ **根级文件夹的 Path 就是它自己的名字**(没有前导 "/")——
//
//	前端判"是不是根级"要看 ParentID,不要看 Path。
//
// ⚠️ **祖先链另有一个字段**(Ancestors,不含自身)—— 面包屑要的是"根 → 直接上级
//
//	→ 当前",而 Path 只有一条线,拼不出中间那几级。
func (t *folderTree) path(id uint64) string {
	n, ok := t.nodes[id]
	if !ok {
		return ""
	}
	anc := t.ancestors(id)
	names := make([]string, 0, len(anc)+1)
	for _, a := range anc {
		names = append(names, a.Name)
	}
	names = append(names, n.Name)
	return strings.Join(names, "/")
}

// childIDs 直接子级 id(按 id 升序)
func (t *folderTree) childIDs(id uint64) []uint64 {
	return t.children[id]
}

// childCount 直接子级数量(**不含更深层**)
func (t *folderTree) childCount(id uint64) int {
	return len(t.children[id])
}

// subtree 这个文件夹 + 它下面全部子孙的 id。
//
// ⚠️ **带 visited 集合**,和 ancestors 同一个理由:成环的脏数据会让广度优先
// 遍历**永远停不下来**(每个节点都能再找到它的父节点当子节点)。
func (t *folderTree) subtree(id uint64) []uint64 {
	seen := map[uint64]bool{}
	var out []uint64
	queue := []uint64{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		n, ok := t.nodes[cur]
		if !ok {
			continue
		}
		out = append(out, n.ID)
		queue = append(queue, t.children[cur]...)
	}
	return out
}
