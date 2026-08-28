import { OrdersDashboard } from "../../../orders-dashboard";

export default async function OrderEventsPage({ params }: Readonly<{ params: Promise<{ id: string }> }>) {
  const { id } = await params;
  return <OrdersDashboard orderId={id} tab="events" />;
}
