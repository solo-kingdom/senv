# config-tui 增量

## REMOVED Requirements

### Requirement: 交互式安装与卸载

**Reason**: All 伪组行为翻转——由「不提供整组 install/uninstall 入口」改为「提供、以全部条目为范围」（grill D6-3：改 spec 承认已交付的实现能力）。整块需求重写，以新需求「安装与卸载入口」接替，原 scenario「All 伪组无整组操作」描述的行为不再存在，故整块退役而非局部修改。

**Migration**: 行为承接见本文件 ADDED「安装与卸载入口」；单条/整组/计划预览/changed 确认语义全部保留，仅 All 伪组范围由禁止变为提供。

## ADDED Requirements

### Requirement: 安装与卸载入口

TUI 与经典菜单 SHALL 提供 install 与 uninstall 入口，作用于选中的单条配置或分组。执行前 SHALL 展示操作计划（动作、目标路径、原因），用户确认后才执行。TUI 中整组作用域 SHALL 锚定左侧分组栏：当焦点在分组栏时，install/uninstall 作用于选中分组——选中真实分组时范围为该分组条目，选中 All 伪组时范围为全部条目（计划逐条列出，语义与真实分组一致）。焦点在条目列表时，单条 install/uninstall 作用于光标所在条目，整组 install/uninstall 作用于该条目所属分组。

#### Scenario: TUI 中安装单条配置
- **WHEN** 在 config tab 条目列表对某条配置触发 install
- **THEN** 弹出计划预览，确认后执行并反馈结果

#### Scenario: TUI 中从分组栏安装整组
- **WHEN** 焦点在分组栏且选中真实分组，触发整组 install
- **THEN** 展示该组的 install 计划预览，确认后执行

#### Scenario: All 伪组整组操作
- **WHEN** 焦点在分组栏且选中 All，触发整组 install/uninstall
- **THEN** 弹出以全部条目为范围的计划预览，确认后执行

#### Scenario: 经典菜单中按组安装
- **WHEN** 在交互菜单选择按组 install
- **THEN** 展示该组计划，确认后执行
