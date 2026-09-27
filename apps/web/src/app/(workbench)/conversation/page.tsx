import { ConversationView } from "../../../features/conversation/conversation-view";

export default function ConversationPage(): React.JSX.Element {
  return (
    <main data-page="conversation">
      <header className="page-header">
        <h1>对话</h1>
        <p className="page-lede">
          用一句话创建、查询或删除待办,也可以直接聊天;会话自动保存,可随时切换并回看历史。
        </p>
      </header>
      <ConversationView />
    </main>
  );
}
