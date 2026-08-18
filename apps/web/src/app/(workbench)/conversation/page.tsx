import { ChatPanel } from "../../../features/conversation/chat-panel";

export default function ConversationPage(): React.JSX.Element {
  return (
    <main data-page="conversation">
      <h1>对话</h1>
      <ChatPanel />
    </main>
  );
}
