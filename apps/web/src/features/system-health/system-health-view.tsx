import type {
  HealthComponent,
  HealthStatus,
  SystemHealthReport,
} from "./types";

const statusContent: Record<HealthStatus, { heading: string; label: string }> =
  {
    healthy: { heading: "所有系统运行正常", label: "正常" },
    degraded: { heading: "部分系统需要关注", label: "降级" },
    unavailable: { heading: "健康状态暂不可用", label: "不可用" },
  };

export function SystemHealthView({
  report,
}: {
  report: SystemHealthReport;
}): React.JSX.Element {
  const content = statusContent[report.status];
  const components: Array<{ name: string; component: HealthComponent }> = [
    {
      name: "Web",
      component: { status: "healthy", checkedAt: report.checkedAt },
    },
    { name: "API", component: report.components.api },
    { name: "PostgreSQL", component: report.components.database },
    { name: "Worker", component: report.components.worker },
  ];

  return (
    <main className="system-health" data-system-status={report.status}>
      <header>
        <h1>{content.heading}</h1>
        <p>
          <span className={`status-badge status-${report.status}`}>
            {content.label}
          </span>
        </p>
        <p>
          检查时间{" "}
          <time dateTime={report.checkedAt}>
            {new Date(report.checkedAt).toLocaleString()}
          </time>
        </p>
      </header>
      <section aria-label="服务状态" className="health-grid">
        {components.map(({ name, component }) => (
          <article className="health-card" key={name}>
            <h2>{name}</h2>
            <p>
              <span
                className={`status-badge status-${
                  component.status === "healthy" ? "healthy" : "unavailable"
                }`}
              >
                {component.status === "healthy" ? "正常" : "异常"}
              </span>
            </p>
            <p>
              更新于{" "}
              <time dateTime={component.checkedAt}>
                {new Date(component.checkedAt).toLocaleString()}
              </time>
            </p>
          </article>
        ))}
      </section>
    </main>
  );
}
