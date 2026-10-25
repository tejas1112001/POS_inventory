import React, { useState, useEffect } from 'react';
import PageHeader from '../components/forms/PageHeader';
import FormFooter from '../components/forms/FormFooter';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import FormStepper from '../components/forms/FormStepper';
import { useFormWizard } from '../hooks/useFormWizard';
import { useNavigate } from 'react-router-dom';
import { usePos } from '../hooks/usePos';
import { api } from '../services/api';
import {
  PlusCircleIcon,
  XMarkIcon
} from '@heroicons/react/24/outline';
import { currencySymbol, formatCurrency } from '@/lib/format';

const SALES_RETURN_STEPS = [
  { id: 'info', title: 'Return Info', description: 'Customer and date' },
  { id: 'items', title: 'Return Items', description: 'Products to return' },
  { id: 'payment', title: 'Payment', description: 'Refund details' },
];

const AddSalesReturn = () => {
  const [customers, setCustomers] = useState([]);
  const [products, setProducts] = useState([]);
  const [stores, setStores] = useState([]);
  const [sales, setSales] = useState([]);
  const [returnItems, setReturnItems] = useState([]);
  const { currentUser } = usePos();
  const { currentStep, goNext, goBack, goToStep } = useFormWizard(3);
  
  const navigate = useNavigate();

  useEffect(() => {
    const fetchOptions = async () => {
      try {
        const [customersResponse, productsResponse, storesResponse, salesResponse] = await Promise.all([
          api.get('/api/customers'),
          api.get('/api/products'),
          api.get('/api/stores'),
          api.get('/api/sales')
        ]);
        setCustomers(customersResponse);
        setProducts(productsResponse);
        setStores(storesResponse);
        setSales(salesResponse);
      } catch (error) {
        console.error('Error fetching options:', error);
      }
    };
    fetchOptions();
  }, []);

  const generateReference = () => {
    const randomNumber = Math.floor(Math.random() * 1000000);
    return `RET${randomNumber}`;
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    
    try {
      const formData = new FormData(e.target);
      const data = Object.fromEntries(formData.entries());
      
      if (!currentUser || !currentUser.id) throw new Error('Current user ID not found');
      
      // Prepare return items
      const items = [];
      let subtotal = 0;
      
      returnItems.forEach((item, index) => {
        const quantity = parseFloat(data[`quantity_${index}`]) || item.quantity || 0;
        const unitPrice = parseFloat(data[`unitPrice_${index}`]) || item.unitPrice || 0;
        const reason = data[`reason_${index}`] || item.reason || '';
        const total = quantity * unitPrice;
        
        items.push({
          productId: item.productId,
          variantId: item.variantId || null,
          quantity: quantity,
          unitPrice: unitPrice,
          reason: reason,
          total: total
        });
        
        subtotal += total;
      });
      
      // Calculate totals
      const total = subtotal;
      const paid = parseFloat(data.paid) || 0;
      const due = total - paid;
      
      const salesReturnData = {
        reference: data.reference,
        date: data.date || new Date().toISOString().split('T')[0],
        status: data.status || 'Pending',
        payment_status: data.payment_status || 'Unpaid',
        total: total,
        paid: paid,
        due: due,
        customerId: data.customerId,
        storeId: data.storeId,
        saleId: data.saleId || null,
        returnItems: items
      };
      
      await api.post('/api/sales/returns', salesReturnData);
      navigate('/sales-return');
    } catch (error) {
      console.error('Error creating sales return:', error);
    }
  };

  const addReturnItem = () => {
    setReturnItems([...returnItems, { productId: '', variantId: '', quantity: 1, unitPrice: 0, reason: '' }]);
  };

  const removeReturnItem = (index) => {
    const newItems = [...returnItems];
    newItems.splice(index, 1);
    setReturnItems(newItems);
  };

  const updateReturnItem = (index, field, value) => {
    const newItems = [...returnItems];
    newItems[index][field] = value;
    setReturnItems(newItems);
  };

  return (
    <FormPageLayout>
      <PageHeader />

      <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
        <FormCard>
          <FormBody>
            <FormStepper
              steps={SALES_RETURN_STEPS}
              currentStep={currentStep}
              onStepClick={goToStep}
            />

          <div className={currentStep === 0 ? 'block' : 'hidden'}>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Reference <span className="text-red-500">*</span>
                </label>
                <div className="flex">
                  <input 
                    name="reference"
                    type="text" 
                    className="w-full px-3 py-2 border border-gray-600 rounded-l-lg focus:ring-blue-500 focus:border-blue-500"
                    placeholder="Enter reference"
                    required
                  />
                  <button 
                    type="button" 
                    className="px-4 py-2 bg-black text-white rounded-r-lg hover:bg-gray-700"
                    onClick={(e) => {
                      const refInput = e.target.closest('div').querySelector('input[name="reference"]');
                      refInput.value = generateReference();
                    }}
                  >
                    Generate
                  </button>
                </div>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Date <span className="text-red-500">*</span>
                </label>
                <input 
                  name="date"
                  type="date" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  defaultValue={new Date().toISOString().split('T')[0]}
                  required
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Original Sale (Optional Link)
                </label>
                <select 
                  name="saleId" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  onChange={(e) => {
                    const selectedSale = sales.find(s => String(s.id) === e.target.value);
                    if (selectedSale) {
                      const custSelect = document.querySelector('[name="customerId"]');
                      const storeSelect = document.querySelector('[name="storeId"]');
                      if (custSelect && selectedSale.customer_id) custSelect.value = selectedSale.customer_id;
                      if (storeSelect && selectedSale.store_id) storeSelect.value = selectedSale.store_id;
                    }
                  }}
                >
                  <option value="">Select Original Sale</option>
                  {sales.map(sale => (
                    <option key={sale.id} value={sale.id}>
                      {sale.reference || `Sale #${sale.id}`} - {formatCurrency(sale.total)}
                    </option>
                  ))}
                </select>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Customer <span className="text-red-500">*</span>
                </label>
                <select 
                  name="customerId" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  required
                >
                  <option value="">Select Customer</option>
                  {customers.map(customer => (
                    <option key={customer.id} value={customer.id}>{customer.name}</option>
                  ))}
                </select>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Store <span className="text-red-500">*</span>
                </label>
                <select 
                  name="storeId" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  required
                >
                  <option value="">Select Store</option>
                  {stores.map(store => (
                    <option key={store.id} value={store.id}>{store.name}</option>
                  ))}
                </select>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Status <span className="text-red-500">*</span>
                </label>
                <select 
                  name="status" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  required
                >
                  <option value="Pending">Pending</option>
                  <option value="Completed">Completed</option>
                </select>
              </div>
            </div>
          </div>

          <div className={currentStep === 1 ? 'block' : 'hidden'}>
            <div className="mb-4">
              <button 
                type="button" 
                className="flex items-center text-black text-sm p-2 bg-gray-100 rounded-lg hover:bg-gray-200"
                onClick={addReturnItem}
              >
                <PlusCircleIcon className="w-4 h-4 mr-1" />
                Add Return Item
              </button>
            </div>
            
            {returnItems.length > 0 ? (
              <div className="pos-table-wrap">
                <table className="pos-table">
                  <thead>
                    <tr>
                      <th>Product</th>
                      <th>Quantity</th>
                      <th>Unit Price</th>
                      <th>Reason</th>
                      <th>Total</th>
                      <th>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {returnItems.map((item, index) => (
                      <tr key={index}>
                        <td className="px-4 py-3">
                          <select 
                            name={`productId_${index}`}
                            className="w-full px-2 py-1 border border-gray-600 rounded focus:ring-blue-500 focus:border-blue-500"
                            value={item.productId}
                            onChange={(e) => updateReturnItem(index, 'productId', e.target.value)}
                            required
                          >
                            <option value="">Select Product</option>
                            {products.map(product => (
                              <option key={product.id} value={product.id}>{product.name}</option>
                            ))}
                          </select>
                        </td>
                        <td className="px-4 py-3">
                          <input 
                            name={`quantity_${index}`}
                            type="number" 
                            className="w-full px-2 py-1 border border-gray-600 rounded focus:ring-blue-500 focus:border-blue-500"
                            min="1"
                            defaultValue="1"
                            onChange={(e) => updateReturnItem(index, 'quantity', parseFloat(e.target.value) || 0)}
                            required
                          />
                        </td>
                        <td className="px-4 py-3">
                          <input 
                            name={`unitPrice_${index}`}
                            type="number" 
                            className="w-full px-2 py-1 border border-gray-600 rounded focus:ring-blue-500 focus:border-blue-500"
                            step="0.01"
                            min="0"
                            onChange={(e) => updateReturnItem(index, 'unitPrice', parseFloat(e.target.value) || 0)}
                            required
                          />
                        </td>
                        <td className="px-4 py-3">
                          <input 
                            name={`reason_${index}`}
                            type="text" 
                            className="w-full px-2 py-1 border border-gray-600 rounded focus:ring-blue-500 focus:border-blue-500"
                            placeholder="Return reason"
                            onChange={(e) => updateReturnItem(index, 'reason', e.target.value)}
                          />
                        </td>
                        <td className="px-4 py-3">
                          <input 
                            type="text" 
                            className="w-full px-2 py-1 border border-gray-600 rounded bg-gray-100"
                            readOnly
                            value={(item.quantity * item.unitPrice).toFixed(2)}
                          />
                        </td>
                        <td className="px-4 py-3">
                          <button 
                            type="button" 
                            className="p-1 text-red-600 hover:bg-red-50 rounded"
                            onClick={() => removeReturnItem(index)}
                          >
                            <XMarkIcon className="w-5 h-5" />
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="text-center py-4 text-gray-500">
                No items added yet. Click "Add Return Item" to get started.
              </div>
            )}
          </div>

          <div className={currentStep === 2 ? 'block' : 'hidden'}>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Payment Status <span className="text-red-500">*</span>
                </label>
                <select 
                  name="payment_status" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  required
                >
                  <option value="Unpaid">Unpaid</option>
                  <option value="Paid">Paid</option>
                  <option value="Partial">Partial</option>
                </select>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Refund Amount ({currencySymbol()})
                </label>
                <input 
                  name="paid"
                  type="number" 
                  step="0.01"
                  min="0"
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  placeholder="0.00"
                />
              </div>
            </div>
            
            <div className="mt-4 p-4 bg-gray-50 rounded-lg">
              <h3 className="text-sm font-medium text-gray-700 mb-2">Return Summary</h3>
              <div className="space-y-1 text-sm">
                <div className="flex justify-between">
                  <span className="text-gray-600">Total Items:</span>
                  <span className="font-medium">{returnItems.length}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-gray-600">Total Quantity:</span>
                  <span className="font-medium">{returnItems.reduce((sum, item) => sum + (item.quantity || 0), 0)}</span>
                </div>
                <div className="flex justify-between pt-2 border-t">
                  <span className="text-gray-700 font-medium">Total Amount:</span>
                  <span className="font-bold">{formatCurrency(returnItems.reduce((sum, item) => sum + (item.quantity * item.unitPrice || 0), 0))}</span>
                </div>
              </div>
            </div>
          </div>
          </FormBody>

        <FormFooter 
          cancelPath="/sales-return" 
          submitLabel="Create Sales Return"
          showStepNav
          currentStep={currentStep}
          totalSteps={3}
          onBack={goBack}
          onNext={goNext}
        />
        </FormCard>
      </form>
    </FormPageLayout>
  );
};

export default AddSalesReturn;
