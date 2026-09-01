import { ChannelManager } from "../../../features/settings/channel-manager";

export default function SettingsPage(): React.JSX.Element {
  return (
    <main data-page="settings">
      <header className="page-header">
        <h1>设置</h1>
        <p className="page-lede">管理接收提醒的联系方式。</p>
      </header>
      <ChannelManager />
    </main>
  );
}
