import { useState, useCallback } from 'react';

export function useFormWizard(stepCount) {
  const [currentStep, setCurrentStep] = useState(0);

  const goNext = useCallback(() => {
    setCurrentStep((s) => Math.min(s + 1, stepCount - 1));
  }, [stepCount]);

  const goBack = useCallback(() => {
    setCurrentStep((s) => Math.max(s - 1, 0));
  }, []);

  const goToStep = useCallback(
    (index) => {
      if (index >= 0 && index < stepCount) {
        setCurrentStep(index);
      }
    },
    [stepCount]
  );

  return { currentStep, goNext, goBack, goToStep, setCurrentStep };
}

export const FORM_STEPS = {
  product: [
    { id: 'info', title: 'Product Info', description: 'Basic details' },
    { id: 'pricing', title: 'Pricing & Stock', description: 'Price and inventory' },
    { id: 'images', title: 'Images', description: 'Product photos' },
    { id: 'custom', title: 'Custom Fields', description: 'Extra attributes' },
  ],
  sale: [
    { id: 'info', title: 'Sale Info', description: 'Customer and date' },
    { id: 'items', title: 'Order Items', description: 'Products in sale' },
    { id: 'payment', title: 'Payment', description: 'Payment details' },
  ],
  purchase: [
    { id: 'info', title: 'Purchase Info', description: 'Supplier and date' },
    { id: 'items', title: 'Purchase Items', description: 'Products ordered' },
  ],
  customer: [
    { id: 'info', title: 'Customer Info', description: 'Contact details' },
    { id: 'image', title: 'Profile Image', description: 'Upload photo' },
  ],
  supplier: [
    { id: 'info', title: 'Supplier Info', description: 'Contact details' },
    { id: 'image', title: 'Profile Image', description: 'Upload photo' },
  ],
  salesReturn: [
    { id: 'info', title: 'Return Info', description: 'Customer and date' },
    { id: 'items', title: 'Return Items', description: 'Products to return' },
    { id: 'payment', title: 'Payment', description: 'Refund details' },
  ],
};
