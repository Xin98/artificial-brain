import { AccountPanel } from "../../../../../features/investment/account-panel";
export default async function AccountPage({
  params,
}: {
  params: Promise<{ accountId: string }>;
}) {
  const { accountId } = await params;
  return (
    <main data-page="investment">
      <AccountPanel accountId={accountId} />
    </main>
  );
}
