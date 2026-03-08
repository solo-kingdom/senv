# 项目名称
BINARY_NAME=senv

# 安装目录（用户目录下的 bin）
PREFIX ?= $(HOME)/.local
INSTALL_DIR = $(PREFIX)/bin

# Go 参数
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

# 主程序路径
MAIN_PATH=./main.go

.PHONY: all build clean install uninstall test

all: build

# 编译项目
build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BINARY_NAME)"

# 清理编译产物
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	@echo "Clean complete"

# 运行测试
test:
	$(GOTEST) -v ./...

# 安装到用户目录
install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_DIR)..."
	@mkdir -p $(INSTALL_DIR)
	@cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installation complete!"
	@echo ""
	@echo "Binary installed to: $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
	@echo "Make sure $(INSTALL_DIR) is in your PATH."
	@echo "Add the following to your shell configuration (~/.bashrc, ~/.zshrc, etc.):"
	@echo "  export PATH=\"\$$PATH:$(INSTALL_DIR)\""

# 卸载
uninstall:
	@echo "Uninstalling $(BINARY_NAME) from $(INSTALL_DIR)..."
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Uninstallation complete!"

# 重新安装
reinstall: uninstall install
