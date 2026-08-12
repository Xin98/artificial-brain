# 使用 Go 模块化单体承载核心业务

核心业务采用 Go 模块化单体，Next.js 仅承载 Web 交互，PostgreSQL 保存业务事实。相比 Spring Boot，我们选择更小的运行时和更简单的私域交付；相比 TypeScript 全栈，我们接受双语言成本，以换取清晰的后端所有权和长期运行稳定性。模块内部使用 domain、application、adapters 分层，只有组合入口了解具体适配器；首版不拆微服务。

