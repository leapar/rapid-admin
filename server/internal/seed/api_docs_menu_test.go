// API 文档**不再进菜单**的守门网(2026-11-03 · M179 后用户拍板)
//
// 历史沿革(为什么这个文件两次出现、方向相反):
//   2026-11-01 §8 G5:menu.apiDocs 作为**顶级菜单**进 seed —— 当时帮助按钮还是
//   哑弹,菜单是后台里唯一的 swagger 入口,「文档存在但没人知道在哪」。
//   2026-11-03 M179:右上角帮助按钮改成下拉菜单,**两个入口**都挂在那里
//   (「API 文档」跳 /api-docs,「使用教程」新标签页开 /docs GitBook);
//   用户随后拍板:左侧菜单里的「API 文档」要去掉。
//
// 为什么入口属于"帮助"、不属于"导航":
//   文档是**随处可得**的东西(在哪个页面都可能想查一下接口),不是信息架构里的
//   一个层级;挂全局帮助下拉 = 任何页面两次点击可达。菜单则是导航结构,
//   放一个"跳到别的端点"的顶级项,把它和业务域并列,语义是错的。
//
// 本文件钉住:menuList() 里**不许再有** menu.apiDocs。
// 页面本身保留(web/src/router/routes.tsx 的 'api-docs' 独立路由,
// 不依赖菜单渲染);/swagger 端点后面的 BasicAuth 闸不变(internal/pkg/swaggerx)。
package seed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestApiDocsMenu_NotInMenuList 菜单表里不许再有 API 文档
func TestApiDocsMenu_NotInMenuList(t *testing.T) {
	for _, m := range menuList() {
		assert.NotEqual(t, "menu.apiDocs", m.Code,
			"menu.apiDocs 不该回到菜单 —— 入口在右上角帮助按钮下拉(helpMenu.ts);"+
				"要恢复菜单请先撤销 2026-11-03 的拍板,而不是只加一行 seed")
		assert.NotEqual(t, "/api-docs", m.Path,
			"任何菜单都不该指向 /api-docs(那是帮助下拉专用的独立路由)")
	}
}
