import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import EditProductForm from '@/components/forms/EditProductForm';

// EditProductSheet: the existing product edit form in a sheet (S8).
export default function EditProductSheet({ product, onClose, onSaved }) {
  return (
    <Dialog open={!!product} onOpenChange={(o) => !o && onClose()}>
      {product && (
        <DialogContent sheet className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>Edit product</DialogTitle>
            <DialogDescription>{product.name}</DialogDescription>
          </DialogHeader>
          <EditProductForm
            product={product}
            onSaved={() => {
              onSaved?.();
              onClose();
            }}
            onCancel={onClose}
          />
        </DialogContent>
      )}
    </Dialog>
  );
}
