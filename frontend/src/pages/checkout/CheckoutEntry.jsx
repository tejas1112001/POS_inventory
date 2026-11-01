import { Loader2 } from 'lucide-react';
import POS from '@/pages/POS';
import { useShopSettings } from '@/hooks/useShopSettings';
import Checkout from './Checkout';

// CheckoutEntry (/pos): the new checkout when the shop turned it on in
// Settings (newCheckout), otherwise the old POS screen. A role that cannot
// read settings gets the old screen.
export default function CheckoutEntry() {
  const { settings, error } = useShopSettings();
  if (!settings && !error) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-muted-foreground">
        <Loader2 className="size-5 animate-spin" aria-hidden="true" /> Loading the checkout…
      </div>
    );
  }
  return settings?.newCheckout ? <Checkout /> : <POS />;
}
