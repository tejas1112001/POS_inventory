// Return policy (industry P1 item 9, A18).

export const CONDITIONS = [
  { value: 'resellable', label: 'Resellable' },
  { value: 'damaged', label: 'Damaged' },
  { value: 'defective', label: 'Defective' },
];

// Today's rules (DB16): what a shop has until it changes the policy.
export const DEFAULT_POLICY = {
  refund: true,
  storeCredit: false,
  exchange: false,
  timeLimitDays: null,
  requireLinkedSale: false,
  conditions: false,
  restockConditions: ['resellable'],
};
