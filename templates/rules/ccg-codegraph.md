# CodeGraph MCP 使用指南

## 何时使用 `codegraph_explore`

CodeGraph 是本地预索引的代码知识图谱（SQLite + Tree-sitter），通过 MCP 暴露 `codegraph_explore` 单一工具。**在以下场景优先使用它，而非 grep/Read/Glob 循环**：

| 场景 | 示例查询 |
|------|----------|
| 函数/类的调用链追踪 | "handleSubmit 被哪些组件调用" |
| 修改影响范围（blast radius）| "改 UserService.create 会影响哪些文件" |
| 架构/流程理解 | "请求从 router 到 database 的完整流转" |
| 跨模块依赖分析 | "AuthMiddleware 依赖哪些模块" |
| 读取已知符号的源码 + 上下文 | 直接传文件名或函数名，返回带行号的源码 + 调用关系 |

## 与 fast-context 的分工

- **CodeGraph** → 结构性查询（调用链、依赖、影响范围、精确符号查找）—— 本地图谱，零网络
- **fast-context** → 语义模糊搜索（"处理用户登录的逻辑在哪"）—— 远端 AI 语义匹配
- **grep/Glob** → 精确文本匹配（已知变量名/字符串的全局查找替换）

## 查询技巧

- 传入具体的符号名效果最佳（如 `UserService.create` 而非 "创建用户的代码"）
- 追踪调用流时，给出起点和终点符号名（如 `handleRequest renderResponse`）
- 返回的源码已带行号，可直接用于 Edit，无需再 Read
- 编辑文件后注意 staleness 提示——有 ⚠️ 标记的文件应直接 Read 而非信任缓存

## 反模式（避免）

- ❌ 先 grep 找到符号再用 CodeGraph 验证（直接用 CodeGraph）
- ❌ 手动拼接调用链（一次 `codegraph_explore` 自动解析，包括动态分派）
- ❌ 对 CodeGraph 返回的源码再用 Read 重新读取（已是同等内容）

## 前提条件

项目根目录须有 `.codegraph/` 索引目录（用户运行 `codegraph init` 创建）。若无索引，CodeGraph 工具会报告此状态，此时回退到 fast-context / grep。
