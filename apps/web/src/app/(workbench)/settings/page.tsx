import { ChannelManager } from "../../../features/settings/channel-manager";

export default function SettingsPage(): React.JSX.Element {
  return (
    <main data-page="settings">
      <h1>设置</h1>
      <ChannelManager />
    </main>
  );
}
