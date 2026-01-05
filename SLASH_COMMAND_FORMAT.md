# Slash Command 格式要求

## 问题背景

当 slash commands 放在子目录下（如 `commands/ccg/`）时，Claude Code CLI 的识别行为与放在 `~/.claude/commands/` 下不同。

### 症状
- **子目录**：`commands/ccg/*.md` - 只能识别部分命令（3个）
- **全局目录**：`~/.claude/commands/*.md` - 可以正常识别所有命令

### 根本原因

**子目录中的 slash commands 必须包含 YAML frontmatter**，否则 Claude Code CLI 无法正确识别。

---

## 解决方案

### 必需格式

每个 `.md` 命令文件必须以 YAML frontmatter 开头：

```markdown
---
description: 命令的简短描述
---

## Usage
...
```

### 完整示例

```markdown
---
description: 多模型技术分析（根据配置并行），交叉验证后综合见解
---

## Usage
`/analyze <QUESTION_OR_TASK>`

## Context
- Question or analysis task: $ARGUMENTS
- This command triggers multi-model analysis without code changes.
...
```

### 扩展格式（权限控制）

对于需要明确权限控制的命令（如 Git 操作），可以添加 `allowed-tools` 字段：

```markdown
---
description: 仅用 Git 分析改动并自动生成 conventional commit 信息
allowed-tools: Read(**), Exec(git status, git diff, git add, git commit), Write(.git/COMMIT_EDITMSG)
---

## 用法
...
```

---

## 权限控制字段说明

### `allowed-tools` 格式

```yaml
allowed-tools: Read(**), Exec(command1, command2), Write(path)
```

### 示例

1. **Git 提交命令**：
   ```yaml
   allowed-tools: Read(**), Exec(git status, git diff, git add, git commit), Write(.git/COMMIT_EDITMSG)
   ```

2. **Git 回滚命令**：
   ```yaml
   allowed-tools: Read(**), Exec(git fetch, git branch, git log, git reset, git revert), Write()
   ```

3. **项目初始化**：
   ```yaml
   allowed-tools: Read(**), Write(CLAUDE.md, **/CLAUDE.md)
   ```

---

## 验证方法

### 1. 检查 frontmatter

```bash
# 检查所有命令文件的开头
cd commands/ccg
for f in *.md; do
  echo "=== $f ==="
  head -3 "$f"
done
```

### 2. 测试命令识别

```bash
# 在 Claude Code 中输入
/ccg:
# 按 Tab 键查看自动补全列表
```

### 3. 检查命令数量

```bash
# 应该显示所有 17 个命令
claude --list-commands | grep ccg
```

---

## 最佳实践

### ✅ 推荐做法

1. **始终添加 frontmatter**：即使命令在全局目录下，也建议添加 frontmatter 以保持一致性
2. **描述清晰简洁**：`description` 字段应该在 1-2 行内说明命令用途
3. **权限最小化**：只在 `allowed-tools` 中列出必需的工具和路径
4. **使用通配符**：
   - `Read(**)` - 允许读取所有文件
   - `Exec(git *)` - 允许所有 git 命令
   - `Write()` - 禁止写入（空列表）

### ❌ 避免的做法

1. **省略 frontmatter**：会导致子目录中的命令无法识别
2. **描述过长**：`description` 字段不要超过 3 行
3. **过度权限**：不要赋予不必要的 `Write` 权限

---

## 迁移指南

### 从无 frontmatter 迁移

如果你的旧命令文件没有 frontmatter：

```bash
# 旧格式
## Usage
/my-command

...

# 新格式（在文件开头添加）
---
description: 我的命令描述
---

## Usage
/my-command

...
```

### 批量添加 frontmatter

```bash
#!/bin/bash
for f in commands/ccg/*.md; do
  # 提取命令名称
  cmd=$(basename "$f" .md)

  # 创建临时文件
  {
    echo "---"
    echo "description: TODO: 添加 $cmd 的描述"
    echo "---"
    echo ""
    cat "$f"
  } > "$f.tmp"

  mv "$f.tmp" "$f"
done
```

---

## 常见问题

### Q: 为什么全局目录不需要 frontmatter？

A: Claude Code CLI 对全局目录（`~/.claude/commands/`）有特殊处理，会自动从文件名和内容中推断命令信息。但子目录需要明确的元数据。

### Q: `allowed-tools` 是必需的吗？

A: 不是必需的。只有 `description` 是必需的。`allowed-tools` 仅用于明确权限控制。

### Q: 可以在子目录下再创建子目录吗？

A: 可以，但每一层的命令文件都必须有 frontmatter。例如 `commands/ccg/agents/*.md` 也需要 frontmatter。

### Q: frontmatter 支持哪些字段？

A: 已知支持的字段：
- `description` (必需)
- `allowed-tools` (可选)
- 其他字段可能在未来版本中支持

---

## 参考

- [Claude Code 官方文档](https://github.com/anthropics/claude-code)
- [本项目的命令示例](./commands/ccg/)
- [YAML Frontmatter 规范](https://yaml.org/spec/1.2.2/)
