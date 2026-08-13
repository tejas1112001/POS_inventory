import React, { useState, useEffect } from 'react';
import PageHeader from '../components/forms/PageHeader';
import FormFooter from '../components/forms/FormFooter';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import FormStepper from '../components/forms/FormStepper';
import { useFormWizard, FORM_STEPS } from '../hooks/useFormWizard';
import { useNavigate } from 'react-router-dom';
import { usePos } from '../hooks/usePos';
import { api } from '../services/api';
import {
  InformationCircleIcon,
  DocumentTextIcon,
  PlusCircleIcon,
  XMarkIcon
} from '@heroicons/react/24/outline';

const AddSales = () => {
  const [customers, setCustomers] = useState([]);
  const [products, setProducts] = useState([]);
  const [orderItems, setOrderItems] = useState([]);
  const { currentUser } = usePos();
  const { currentStep, goNext, goBack, goToStep } = useFormWizard(3);
  
  const navigate = useNavigate();

  useEffect(() => {
    const fetchOptions = async () => {
      try {
        const [customersResponse, productsResponse] = await Promise.all([
          api.get('/api/customers'),
          api.get('/api/products')
        ]);
        setCustomers(customersResponse);
        setProducts(productsResponse);
      } catch (error) {
        console.error('Error fetching options:', error);
      }
    };
    fetchOptions();
  }, []);

  const generateReference = () => {
    const randomNumber = Math.floor(Math.random() * 1000000);
    return `REF${randomNumber}`;
  };

  // Update the handleSubmit function
const handleSubmit = async (e) => {
  e.preventDefault();
  
  try {
    const formData = new FormData(e.target);
    const data = Object.fromEntries(formData.entries());
    
    if (!currentUser || !currentUser.id) throw new Error('Current user ID not found');
    
    // Prepare order items
    const items = [];
    let subtotal = 0;
    
    orderItems.forEach((item, index) => {
      const quantity = parseFloat(data[`quantity_${index}`]) || 0;
      const unitPrice = parseFloat(data[`unitPrice_${index}`]) || 0;
      const total = quantity * unitPrice;
      
      items.push({
        productId: item.productId,
        quantity: quantity,
        unitPrice: unitPrice,
        total: total
      });
      
      subtotal += total;
    });
    
    // Calculate totals
    const discount = parseFloat(data.discount) || 0;
    const tax = parseFloat(data.tax) || 0;
    const shipping = parseFloat(data.shipping) || 0;
    const total = subtotal - discount + tax + shipping;
    const paid = parseFloat(data.paid) || 0;
    const due = total - paid;
    
    const saleData = {
      reference: data.reference,
      date: data.date || new Date().toISOString().split('T')[0],
      status: data.status || 'Pending',
      payment_status: data.payment_status || 'Unpaid',
      payment_method: data.payment_method || '',
      subtotal: subtotal,
      discount: discount,
      tax: tax,
      shipping: shipping,
      total: total,
      paid: paid,
      due: due,
      note: data.note || '',
      customerId: data.customerId,
      userId: currentUser.id,
      orderItems: items
    };
    
    await api.post('/api/sales', saleData);
    navigate('/sales');
  } catch (error) {
    console.error('Error creating sale:', error);
  }
};

  const addOrderItem = () => {
    setOrderItems([...orderItems, { productId: '', variantId: '', quantity: 1, unitPrice: 0 }]);
  };

  const removeOrderItem = (index) => {
    const newItems = [...orderItems];
    newItems.splice(index, 1);
    setOrderItems(newItems);
  };

  const updateOrderItem = (index, field, value) => {
    const newItems = [...orderItems];
    newItems[index][field] = value;
    setOrderItems(newItems);
  };

  return (
    <FormPageLayout>
      <PageHeader />

      <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
        <FormCard>
          <FormBody>
            <FormStepper
              steps={FORM_STEPS.sale}
              currentStep={currentStep}
              onStepClick={goToStep}
            />

          {currentStep === 0 && (
          <div>
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
                    className="px-4 py-2 bg-black text-white rounded-r-lg hover:bg-blue-700"
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
            
            <div className="mt-6">
              <label className="block text-sm font-medium text-gray-700 mb-2">
                Note
              </label>
              <textarea 
                name="note"
                className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500 min-h-[100px]"
                placeholder="Enter sale note"
              ></textarea>
            </div>
          </div>
          )}

          {currentStep === 1 && (
          <div>
            <div className="mb-4">
              <button 
                type="button" 
                className="flex items-center text-black text-sm p-2 bg-gray-100 rounded-lg hover:bg-gray-200"
                onClick={addOrderItem}
              >
                <PlusCircleIcon className="w-4 h-4 mr-1" />
                Add Item
              </button>
            </div>
            
            {orderItems.length > 0 ? (
              <div className="pos-table-wrap">
                <table className="pos-table">
                  <thead>
                    <tr>
                      <th>Product</th>
                      <th>Quantity</th>
                      <th>Unit Price</th>
                      <th>Total</th>
                      <th>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {orderItems.map((item, index) => (
                      <tr key={index}>
                        <td className="px-4 py-3">
                          <select 
                            name={`productId_${index}`}
                            className="w-full px-2 py-1 border border-gray-600 rounded focus:ring-blue-500 focus:border-blue-500"
                            value={item.productId}
                            onChange={(e) => updateOrderItem(index, 'productId', e.target.value)}
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
                            required
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
                            onClick={() => removeOrderItem(index)}
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
                No items added yet. Click "Add Item" to get started.
              </div>
            )}
          </div>
          )}

          {currentStep === 2 && (
          <div>
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
                  <option value="Overdue">Overdue</option>
                </select>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Payment Method
                </label>
                <select 
                  name="payment_method" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                >
                  <option value="">Select Method</option>
                  <option value="Cash">Cash</option>
                  <option value="Card">Card</option>
                  <option value="Bank Transfer">Bank Transfer</option>
                  <option value="Digital Wallet">Digital Wallet</option>
                </select>
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Discount (₹)
                </label>
                <input 
                  name="discount"
                  type="number" 
                  step="0.01"
                  min="0"
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  placeholder="0.00"
                />
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Tax (₹)
                </label>
                <input 
                  name="tax"
                  type="number" 
                  step="0.01"
                  min="0"
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  placeholder="0.00"
                />
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Shipping (₹)
                </label>
                <input 
                  name="shipping"
                  type="number" 
                  step="0.01"
                  min="0"
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  placeholder="0.00"
                />
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Paid Amount (₹)
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
          </div>
          )}
          </FormBody>

        <FormFooter 
          cancelPath="/sales" 
          submitLabel="Create Sale"
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

export default AddSales;