import React from 'react';
import { NavLink } from 'react-router-dom';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Button } from '@/components/ui/button';

const FormFooter = ({
  cancelPath,
  submitLabel = 'Save',
  cancelLabel = 'Cancel',
  disabled = false,
  currentStep,
  totalSteps,
  onBack,
  onNext,
  showStepNav = false,
}) => {
  const isFirstStep = currentStep === 0;
  const isLastStep = currentStep === undefined || currentStep === totalSteps - 1;

  return (
    <div className="sticky bottom-0 z-10 flex items-center justify-between gap-2 border-t bg-card/95 px-3 py-2.5 backdrop-blur-sm sm:px-4">
      <div>
        {showStepNav && !isFirstStep && (
          <Button type="button" variant="outline" onClick={onBack}>
            <ChevronLeft className="mr-1 size-4" />
            Back
          </Button>
        )}
      </div>

      <div className="flex gap-3">
        <Button variant="outline" asChild>
          <NavLink to={cancelPath}>{cancelLabel}</NavLink>
        </Button>

        {/* Distinct keys: React must not turn the clicked Next button into
            the submit button mid-click, or the browser submits the form. */}
        {showStepNav && !isLastStep ? (
          <Button key="next" type="button" onClick={onNext}>
            Next
            <ChevronRight className="ml-1 size-4" />
          </Button>
        ) : (
          <Button key="submit" type="submit" disabled={disabled}>
            {submitLabel}
          </Button>
        )}
      </div>
    </div>
  );
};

export default FormFooter;
