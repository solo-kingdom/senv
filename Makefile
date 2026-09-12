# 项目名称
BINARY_NAME=senv

# 版本信息
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X github.com/wii/senv/cmd.Version=$(VERSION) -X github.com/wii/senv/cmd.BuildTime=$(BUILD_TIME)"

# 安装目录
PREFIX?=$(HOME)/.local
INSTALL_DIR=$(PREFIX)/bin

# Go 命令
GOCMD=go
GOBUILD=$(GOCMD) build $(LDFLAGS)
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
# 额外的 go test 参数（如 -count=1 禁用结果缓存）；默认留空以复用缓存
GOTESTFLAGS?=
GOGET=$(GOCMD) get
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# 主程序路径
MAIN_PATH=./main.go

# 颜色输出
GREEN=\033[0;32m
YELLOW=\033[0;33m
RED=\033[0;31m
NC=\033[0m # No Color

.PHONY: all build clean install uninstall test test-race coverage coverage-summary lint fmt vet check check-fast help

all: build

# 编译项目
build:
	@printf "%b\n" "$(GREEN)Building $(BINARY_NAME)...$(NC)"
	$(GOBUILD) -o $(BINARY_NAME) $(MAIN_PATH)
	@printf "%b\n" "$(GREEN)Build complete: $(BINARY_NAME)$(NC)"

# 编译所有平台
build-all: build-linux build-darwin build-windows

build-linux:
	@printf "%b\n" "$(GREEN)Building for Linux...$(NC)"
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o bin/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)

build-darwin:
	@printf "%b\n" "$(GREEN)Building for macOS...$(NC)"
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o bin/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

build-windows:
	@printf "%b\n" "$(GREEN)Building for Windows...$(NC)"
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o bin/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)

# 编译 senv-server
build-server:
	@printf "%b\n" "$(GREEN)Building senv-server...$(NC)"
	$(GOCMD) build -o senv-server-bin ./senv-server
	@printf "%b\n" "$(GREEN)Build complete: senv-server-bin$(NC)"

# 清理编译产物
clean:
	@printf "%b\n" "$(YELLOW)Cleaning...$(NC)"
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -rf bin/
	rm -f coverage.out coverage.html
	@printf "%b\n" "$(YELLOW)Clean complete$(NC)"

# 运行测试（默认不带 -race：快；提交前用 test-race 或 check-full）
test:
	@printf "%b\n" "$(GREEN)Running tests...$(NC)"
	$(GOTEST) $(GOTESTFLAGS) ./...

# 运行测试（race detector，慢约 10 倍，建议做提交前/CI 全量门禁）
test-race:
	@printf "%b\n" "$(GREEN)Running tests with race detector...$(NC)"
	$(GOTEST) -race $(GOTESTFLAGS) ./...

# 运行测试并生成覆盖率报告（不带 -race，避免重复放大 PBKDF2 成本）
coverage:
	@printf "%b\n" "$(GREEN)Running tests with coverage...$(NC)"
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@printf "%b\n" "$(GREEN)Coverage report generated: coverage.html$(NC)"

# 查看覆盖率摘要
coverage-summary:
	@printf "%b\n" "$(GREEN)Coverage summary...$(NC)"
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -func=coverage.out

# 代码格式化
fmt:
	@printf "%b\n" "$(GREEN)Formatting code...$(NC)"
	$(GOFMT) ./...

# 静态检查
vet:
	@printf "%b\n" "$(GREEN)Running go vet...$(NC)"
	$(GOVET) ./...

# 综合检查（需要安装 golangci-lint）
lint:
	@printf "%b\n" "$(GREEN)Running linter...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		printf "%b\n" "$(YELLOW)golangci-lint not installed, running go vet instead...$(NC)"; \
		$(GOVET) ./...; \
	fi

# 安装到用户目录
install: build
	@printf "%b\n" "$(GREEN)Installing $(BINARY_NAME) to $(INSTALL_DIR)...$(NC)"
	@mkdir -p $(INSTALL_DIR)
	# 目标可能正在运行，原位覆盖会 ETXTBSY；先 unlink，旧进程继续用旧 inode
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_DIR)/$(BINARY_NAME)
	@printf "%b\n" "$(GREEN)Installation complete!$(NC)"
	@echo ""
	@echo "Binary installed to: $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
	@echo "Make sure $(INSTALL_DIR) is in your PATH."
	@echo "Add the following to your shell configuration (~/.bashrc, ~/.zshrc, etc.):"
	@echo "  export PATH=\"\$$PATH:$(INSTALL_DIR)\""

# 卸载
uninstall:
	@printf "%b\n" "$(YELLOW)Uninstalling $(BINARY_NAME) from $(INSTALL_DIR)...$(NC)"
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@printf "%b\n" "$(YELLOW)Uninstallation complete!$(NC)"

# 重新安装
reinstall: uninstall install

# 开发模式（热编译，需要安装 reflex）
dev:
	@if command -v reflex >/dev/null 2>&1; then \
		reflex -g '\.go$$' -s -- sh -c 'make build && ./$(BINARY_NAME)'; \
	else \
		printf "%b\n" "$(RED)reflex not installed. Install with: go install github.com/cespare/reflex@latest$(NC)"; \
	fi

# 检查依赖更新
deps:
	@printf "%b\n" "$(GREEN)Checking dependencies...$(NC)"
	$(GOGET) -u ./...
	$(GOCMD) mod tidy

# 安全检查（需要安装 govulncheck）
security:
	@printf "%b\n" "$(GREEN)Running security check...$(NC)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		printf "%b\n" "$(YELLOW)govulncheck not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest$(NC)"; \
	fi

# 运行所有检查
# 完整门禁：fmt + vet + lint + race 全量测试（提交前 / CI 用）
check: fmt vet lint test-race
	@printf "%b\n" "$(GREEN)All checks passed!$(NC)"

# 快速门禁：跳过 race，适合本地迭代（约 check 的 1/3）
check-fast: fmt vet lint test
	@printf "%b\n" "$(GREEN)All quick checks passed!$(NC)"

# 发布前准备
release: clean check build-all
	@printf "%b\n" "$(GREEN)Release build complete!$(NC)"
	@ls -la bin/

# 帮助信息
help:
	@printf "%b\n" "$(GREEN)Senv - 安全环境变量管理工具$(NC)"
	@echo ""
	@printf "%b\n" "$(YELLOW)使用方法:$(NC)"
	@echo "  make [target]"
	@echo ""
	@printf "%b\n" "$(YELLOW)可用目标:$(NC)"
	@printf "%b\n" "  $(GREEN)build$(NC)           - 编译项目"
	@printf "%b\n" "  $(GREEN)build-all$(NC)       - 编译所有平台版本"
	@printf "%b\n" "  $(GREEN)clean$(NC)           - 清理编译产物"
	@printf "%b\n" "  $(GREEN)test$(NC)            - 运行测试（快速，不含 race）"
	@printf "%b\n" "  $(GREEN)test-race$(NC)       - 运行测试（含 race detector）"
	@printf "%b\n" "  $(GREEN)coverage$(NC)        - 运行测试并生成覆盖率报告"
	@printf "%b\n" "  $(GREEN)coverage-summary$(NC) - 显示覆盖率摘要"
	@printf "%b\n" "  $(GREEN)fmt$(NC)             - 格式化代码"
	@printf "%b\n" "  $(GREEN)vet$(NC)             - 运行 go vet"
	@printf "%b\n" "  $(GREEN)lint$(NC)            - 运行代码检查"
	@printf "%b\n" "  $(GREEN)check$(NC)           - 完整门禁 (fmt + vet + lint + test-race)"
	@printf "%b\n" "  $(GREEN)check-fast$(NC)      - 快速门禁 (fmt + vet + lint + test)"
	@printf "%b\n" "  $(GREEN)install$(NC)         - 安装到 $(INSTALL_DIR)"
	@printf "%b\n" "  $(GREEN)uninstall$(NC)       - 从 $(INSTALL_DIR) 卸载"
	@printf "%b\n" "  $(GREEN)reinstall$(NC)       - 重新安装"
	@printf "%b\n" "  $(GREEN)deps$(NC)            - 更新依赖"
	@printf "%b\n" "  $(GREEN)security$(NC)        - 运行安全检查"
	@printf "%b\n" "  $(GREEN)dev$(NC)             - 开发模式（热编译）"
	@printf "%b\n" "  $(GREEN)release$(NC)         - 发布前准备"
	@printf "%b\n" "  $(GREEN)help$(NC)            - 显示帮助信息"
	@echo ""
	@printf "%b\n" "$(YELLOW)变量:$(NC)"
	@echo "  VERSION=$(VERSION)"
	@echo "  PREFIX=$(PREFIX)"
	@echo "  INSTALL_DIR=$(INSTALL_DIR)"
