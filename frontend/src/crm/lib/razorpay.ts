import type { BillingOrder, RazorpayResult } from "@crm/api/endpoints";

// Razorpay Checkout (D-164): the server makes the order; Checkout takes the payment and hands
// back the ids and signature, which the server checks before anything counts as paid.

interface RazorpayInstance {
  open: () => void;
  on: (
    event: "payment.failed",
    cb: (res: { error?: { description?: string } }) => void,
  ) => void;
}
declare global {
  interface Window {
    Razorpay?: new (options: Record<string, unknown>) => RazorpayInstance;
  }
}

let loading: Promise<void> | null = null;

function loadCheckout(): Promise<void> {
  if (window.Razorpay) return Promise.resolve();
  if (!loading) {
    loading = new Promise<void>((resolve, reject) => {
      const s = document.createElement("script");
      s.src = "https://checkout.razorpay.com/v1/checkout.js";
      s.async = true;
      s.onload = () => resolve();
      s.onerror = () => {
        loading = null;
        reject(
          new Error(
            "Couldn't load Razorpay. Check your connection and try again.",
          ),
        );
      };
      document.head.appendChild(s);
    });
  }
  return loading;
}

/** Thrown when the person closes Checkout without paying. */
export class PaymentCancelled extends Error {
  constructor() {
    super("Payment cancelled");
  }
}

export async function payWithRazorpay(
  order: BillingOrder,
): Promise<RazorpayResult> {
  await loadCheckout();
  const Razorpay = window.Razorpay;
  if (!Razorpay)
    throw new Error(
      "Couldn't load Razorpay. Check your connection and try again.",
    );
  return new Promise<RazorpayResult>((resolve, reject) => {
    let failed: string | null = null;
    const rzp = new Razorpay({
      key: order.keyId,
      order_id: order.orderId,
      amount: order.amount,
      currency: order.currency,
      name: order.business,
      description: order.description,
      prefill: order.prefill,
      notes: { members: order.members.join(", ") },
      handler: (res: RazorpayResult) => resolve(res),
      modal: {
        // Closing after a failed attempt reports the failure; otherwise it's a cancel.
        ondismiss: () =>
          failed ? reject(new Error(failed)) : reject(new PaymentCancelled()),
      },
    });
    rzp.on("payment.failed", (res) => {
      failed =
        res.error?.description || "The payment failed. Please try again.";
    });
    rzp.open();
  });
}

export function rupees(n: number): string {
  return `₹${n.toLocaleString("en-IN")}`;
}
