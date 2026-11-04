.PHONY: help build run dev lint test fmt tidy clean seed swag dev-sqlite dev-mysql migrate-sqlite migrate-mysql seed-sqlite seed-mysql docs docs-force docs-check

# 变量
BIN_DIR    := bin
BIN_NAME   := rapid-admin
MAIN_PKG   := ./cmd/server
CONFIG     := configs/config.yaml
DEV_CONFIG := configs/config.dev.yaml

# 默认目标
help:
	@echo "=== rapid-admin ==="
	@echo "  make build        编译二进制到 bin/(手册产物过期时自动重建)"
	@echo "  make docs         构建框架手册(GitBook → docs/site),源没变就跳过"
	@echo "  make docs-force   强制重建框架手册"
	@echo "  make docs-check   检查手册产物是否与源同步(落后则退出码 1)"
	@echo "  make run          本地启动(使用 configs/config.yaml,默认 MySQL)"
	@echo "  make dev          热启动(需安装 air: go install github.com/cosmtrek/air@latest)"
	@echo "  make dev-sqlite   本地启动(SQLite,免装 MySQL;首次自动初始化 config.dev.yaml)"
	@echo "  make dev-mysql    本地启动(MySQL;首次自动初始化 config.yaml)"
	@echo "  make migrate-sqlite  对 SQLite 跑 AutoMigrate"
	@echo "  make migrate-mysql   对 MySQL 跑 AutoMigrate"
	@echo "  make seed-sqlite     对 SQLite 灌种子数据"
	@echo "  make seed-mysql      对 MySQL 灌种子数据"
	@echo "  make seed            兼容旧名:等同 seed-mysql"
	@echo "  make lint       golangci-lint 检查"
	@echo "  make fmt        gofmt 格式化"
	@echo "  make test       go test"
	@echo "  make tidy       go mod tidy"
	@echo "  make swag       生成 swagger 文档"
	@echo "  make clean      清理编译产物"

## build: 编译二进制
##
## ⚠️ **依赖 docs** —— 手册(GitBook)产物是用 go:embed 打进二进制的,
##    所以"源改了没重建" = "二进制里嵌的是旧手册"。docs 目标自己会比对源文件
##    指纹:没变就跳过(不需要 node),变了才真的调 honkit 重建。
build: docs
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BIN_NAME) $(MAIN_PKG)
	@echo "==> build done: $(BIN_DIR)/$(BIN_NAME)"

## docs: 构建框架手册(GitBook → docs/site),源没变就跳过
docs:
	@go run ./cmd/docs build

## docs-force: 强制重建框架手册
docs-force:
	@go run ./cmd/docs force

## docs-check: 检查手册产物是否与源同步(落后则退出码 1)
docs-check:
	@go run ./cmd/docs check

## run: 本地启动(默认 MySQL;想要 SQLite 请用 dev-sqlite)
run:
	@if [ ! -f $(CONFIG) ]; then cp configs/config.example.yaml $(CONFIG); fi
	go run $(MAIN_PKG) -c $(CONFIG)

## dev: 热启动
dev:
	@if [ ! -f $(CONFIG) ]; then cp configs/config.example.yaml $(CONFIG); fi
	air -c .air.toml

## dev-sqlite: 本地启动(SQLite),免装 MySQL
dev-sqlite:
	@if [ ! -f $(DEV_CONFIG) ]; then cp configs/config.dev.yaml.example $(DEV_CONFIG); fi
	@mkdir -p data
	go run $(MAIN_PKG) -c $(DEV_CONFIG)

## dev-mysql: 本地启动(MySQL)
dev-mysql:
	@if [ ! -f $(CONFIG) ]; then cp configs/config.example.yaml $(CONFIG); fi
	go run $(MAIN_PKG) -c $(CONFIG)

## migrate-sqlite: 对 SQLite 跑 AutoMigrate
migrate-sqlite:
	@if [ ! -f $(DEV_CONFIG) ]; then cp configs/config.dev.yaml.example $(DEV_CONFIG); fi
	@mkdir -p data
	go run ./cmd/migrate -config $(DEV_CONFIG)

## migrate-mysql: 对 MySQL 跑 AutoMigrate
migrate-mysql:
	@if [ ! -f $(CONFIG) ]; then cp configs/config.example.yaml $(CONFIG); fi
	go run ./cmd/migrate -config $(CONFIG)

## seed-sqlite: 对 SQLite 灌种子数据
seed-sqlite:
	@if [ ! -f $(DEV_CONFIG) ]; then cp configs/config.dev.yaml.example $(DEV_CONFIG); fi
	@mkdir -p data
	go run ./cmd/seed -config $(DEV_CONFIG)

## seed-mysql: 对 MySQL 灌种子数据
seed-mysql:
	@if [ ! -f $(CONFIG) ]; then cp configs/config.example.yaml $(CONFIG); fi
	go run ./cmd/seed -config $(CONFIG)

## lint: 代码检查
lint:
	golangci-lint run ./...

## fmt: 格式化
fmt:
	gofmt -s -w .

## test: 单元测试
##
## ⚠️ **-timeout 20m 是必须的,别删**。Go 的默认超时是 10 分钟/包,而
## internal/service 在 race 模式下要跑 4 分多钟(每个夹具都要建表,
## 纯 Go 的 modernc.org/sqlite 在 race 模式下慢一个数量级)。默认 10m
## 留给它的余量只有一半,慢一点的机器/CI 上就会被 kill,报
## "Test killed: ran too long (10m0s)" —— 看起来像卡死,其实是超时。
test:
	go test -race -v -timeout 20m ./...

## tidy: 整理 go.mod
tidy:
	go mod tidy

## seed: 种子数据
seed:
	go run scripts/seed.go -c $(CONFIG)

## swag: 用 swag CLI 生成 OpenAPI 文档(遗留路径)
##
## ⚠️ **输出目录必须在本 module 之外** —— swag 生成的代码 import
##    github.com/swaggo/swag,而本仓 go.mod 里没有这个依赖;放在 module 内部
##    (哪怕是 gitignore 的目录)会让 `go build ./...` 直接失败。
## ⚠️ 旧写法是 `-o docs/swag` —— 那个目录现在是**框架手册的源目录**,
##    生成的代码会被 honkit 当手册素材拷进产物。
## 本仓的 OpenAPI 由 internal/pkg/swaggerx 在**运行期**生成(挂 /swagger),
## 这条命令只是"想看看 swag CLI 长什么样"时用的。
swag:
	@if ! command -v swag >/dev/null 2>&1; then \
		echo "install swag: go install github.com/swaggo/swag/cmd/swag@latest"; \
		exit 1; \
	fi
	swag init -g cmd/server/main.go -o ../.tmp/swagger

## clean: 清理
clean:
	rm -rf $(BIN_DIR) logs/ app-logs/
