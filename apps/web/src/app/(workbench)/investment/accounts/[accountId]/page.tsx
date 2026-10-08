import { AccountPanel } from "../../../../../features/investment/account-panel";
export default async function AccountPage({
  params,
  searchParams,
}: {
  params: Promise<{ accountId: string }>;
  searchParams: Promise<{ instrument?: string }>;
}) {
  const { accountId } = await params;
  const { instrument } = await searchParams;
  return (
    <main data-page="investment">
      <AccountPanel
        accountId={accountId}
        prefillInstrumentId={typeof instrument === "string" ? instrument : ""}
      />
    </main>
  );
}
