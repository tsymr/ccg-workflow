# 更新日志

## 2026-01-05 - Slash Command 格式要求文档化

### 问题发现

用户反馈：子目录下的 slash commands（`commands/ccg/*.md`）只能识别出 3 个命令，但移动到 `~/.claude/commands/` 后可以正常识别所有命令。

### 根本原因

**Claude Code CLI 对子目录中的 slash commands 有特殊要求**：必须包含 YAML frontmatter，否则无法正确识别。

### 解决方案

✅ 为所有命令文件添加 YAML frontmatter：

```markdown
---
description: 命令的简短描述
---

## Usage
...
```

### 已完成的工作

1. ✅ 确认所有 17 个命令文件都已包含正确的 frontmatter
2. ✅ 创建详细的格式说明文档：[SLASH_COMMAND_FORMAT.md](./SLASH_COMMAND_FORMAT.md)
3. ✅ 更新 [CLAUDE.md](./CLAUDE.md) 添加格式要求章节
4. ✅ 提供最佳实践和常见问题解答

### 影响范围

- ✅ `commands/ccg/*.md` - 17 个命令文件
- 📋 未来添加的子命令也需要遵循此格式

### 验证方法

```bash
# 检查所有命令的 frontmatter
cd commands/ccg
for f in *.md; do
  echo "=== $f ==="
  head -3 "$f"
done

# 测试命令识别
# 在 Claude Code 中输入 /ccg: 并按 Tab 查看自动补全
```

### 参考文档

- [SLASH_COMMAND_FORMAT.md](./SLASH_COMMAND_FORMAT.md) - 完整格式说明
- [CLAUDE.md](./CLAUDE.md) - 项目主文档

---

## 致谢

感谢用户反馈此问题，帮助改进文档质量！
