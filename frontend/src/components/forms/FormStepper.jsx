import React from 'react';
import { cn } from '@/lib/utils';
import { Check } from 'lucide-react';

export default function FormStepper({ steps, currentStep, onStepClick }) {
  return (
    <nav aria-label="Form progress" className="mb-3">
      <ol className="flex items-center gap-1 overflow-x-auto pb-1">
        {steps.map((step, index) => {
          const isComplete = index < currentStep;
          const isCurrent = index === currentStep;
          const isClickable = onStepClick && index <= currentStep;

          return (
            <li key={step.id} className="flex min-w-0 flex-1 items-center">
              <button
                type="button"
                disabled={!isClickable}
                onClick={() => isClickable && onStepClick(index)}
                className={cn(
                  'flex w-full min-w-0 items-center gap-1.5 rounded-md px-2 py-1.5 text-left transition-colors sm:px-2.5',
                  isCurrent && 'bg-primary/10',
                  isClickable && !isCurrent && 'hover:bg-muted cursor-pointer',
                  !isClickable && 'cursor-default'
                )}
              >
                <span
                  className={cn(
                    'flex size-6 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold transition-colors',
                    isComplete && 'bg-primary text-primary-foreground',
                    isCurrent && !isComplete && 'bg-primary text-primary-foreground ring-2 ring-primary/30 ring-offset-2',
                    !isComplete && !isCurrent && 'bg-muted text-muted-foreground'
                  )}
                >
                  {isComplete ? <Check className="size-3.5" /> : index + 1}
                </span>
                <div className="min-w-0 hidden sm:block">
                  <p
                    className={cn(
                      'truncate text-sm font-medium',
                      isCurrent ? 'text-primary' : isComplete ? 'text-foreground' : 'text-muted-foreground'
                    )}
                  >
                    {step.title}
                  </p>
                  {step.description && (
                    <p className="truncate text-xs text-muted-foreground">{step.description}</p>
                  )}
                </div>
              </button>
              {index < steps.length - 1 && (
                <div
                  className={cn(
                    'mx-1 hidden h-0.5 w-4 shrink-0 sm:block lg:w-8',
                    index < currentStep ? 'bg-primary' : 'bg-border'
                  )}
                />
              )}
            </li>
          );
        })}
      </ol>
      <p className="mt-2 text-sm font-medium text-primary sm:hidden">
        Step {currentStep + 1} of {steps.length}: {steps[currentStep]?.title}
      </p>
    </nav>
  );
}
