import { ChatPanel } from "../../../features/conversation/chat-panel";

export default function ConversationPage(): React.JSX.Element {
  return (
    <main data-page="conversation">
      <header className="page-header">
        <h1>对话</h1>
        <p className="page-lede">用一句话创建待办,或查询、删除已有待办。</p>
      </header>
      <ChatPanel />
    </main>
  );
}
