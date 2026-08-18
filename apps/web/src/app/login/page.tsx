import { LoginForm } from "../../features/auth/login-form";

export default function LoginPage(): React.JSX.Element {
  return (
    <main data-page="login">
      <h1>登录</h1>
      <LoginForm />
    </main>
  );
}
