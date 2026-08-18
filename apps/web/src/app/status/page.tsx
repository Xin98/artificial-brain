import { fetchSystemHealth } from "../../features/system-health/fetch-system-health";
import { SystemHealthView } from "../../features/system-health/system-health-view";
import { apiInternalURL } from "../../shared/server/runtime-config";

export default async function StatusPage(): Promise<React.JSX.Element> {
  const report = await fetchSystemHealth(apiInternalURL(), fetch);

  return <SystemHealthView report={report} />;
}
