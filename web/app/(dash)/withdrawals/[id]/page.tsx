import { WithdrawalDetail } from "../../withdrawal-detail";

export default async function WithdrawalPage({ params }: Readonly<{ params: Promise<{ id: string }> }>) {
  const { id } = await params;
  return <WithdrawalDetail id={id} />;
}
