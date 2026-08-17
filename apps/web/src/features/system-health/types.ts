export type HealthStatus = "healthy" | "degraded" | "unavailable";

export type HealthComponentStatus = "healthy" | "unavailable";

export interface HealthComponent {
  status: HealthComponentStatus;
  checkedAt: string;
  detail?: string;
}

export interface SystemHealthReport {
  status: HealthStatus;
  checkedAt: string;
  correlationId: string;
  components: {
    api: HealthComponent;
    database: HealthComponent;
    worker: HealthComponent;
  };
}
