import { LoginForm } from "../../features/auth/login-form";

export default function LoginPage(): React.JSX.Element {
  return (
    <main data-page="login">
      <div className="login-card">
        <p className="login-brand">
          <span aria-hidden="true" className="workbench-mark">
            ab
          </span>
          <span>Artificial Brain</span>
        </p>
        <h1>登录</h1>
        <p className="login-lede">通过手机号验证码进入你的个人工作台。</p>
        <LoginForm />
      </div>
    </main>
  );
}
