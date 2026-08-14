import { AddressDetail } from "../../address-detail";

export default async function AddressPage({ params }: Readonly<{ params: Promise<{ address: string }> }>) {
  const { address } = await params;
  return <AddressDetail address={address} />;
}
