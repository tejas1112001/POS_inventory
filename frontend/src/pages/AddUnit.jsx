import React, { useState } from 'react';
import Accordion from '../components/forms/Accordion';
import PageHeader from '../components/forms/PageHeader';
import FormFooter from '../components/forms/FormFooter';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import { useNavigate } from 'react-router-dom';
import { usePermission } from '../hooks/usePermission';
import { api } from '../services/api';
import { InformationCircleIcon } from '@heroicons/react/24/outline';

const AddUnit = () => {
  const { hasPermission } = usePermission();
  const navigate = useNavigate();

  const handleSubmit = async (e) => {
    e.preventDefault();
    
    try {
      const formData = new FormData(e.target);
      const data = Object.fromEntries(formData.entries());
      
      await api.post('/api/units', data);
      navigate('/units');
    } catch (error) {
      console.error('Error creating unit:', error);
    }
  };


  
  return (
    <FormPageLayout>
      <PageHeader />

      <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
        <FormCard>
          <FormBody>
          <Accordion 
            title="Unit Information"
            icon={InformationCircleIcon}
            isOpen={true}
            onToggle={() => {}}
          >
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Unit Name <span className="text-red-500">*</span>
                </label>
                <input 
                  name="name"
                  type="text" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  placeholder="Enter unit name"
                  required
                />
              </div>
              
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Short Name <span className="text-red-500">*</span>
                </label>
                <input 
                  name="short_name"
                  type="text" 
                  className="w-full px-3 py-2 border border-gray-600 rounded-lg focus:ring-blue-500 focus:border-blue-500"
                  placeholder="Enter short name"
                  required
                />
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
                  <option value="Active">Active</option>
                  <option value="Inactive">Inactive</option>
                </select>
              </div>
            </div>
          </Accordion>
          </FormBody>

        <FormFooter 
          cancelPath="/units" 
          submitLabel="Add Unit"
          disabled={!hasPermission('units', 'create')}
        />
        </FormCard>
      </form>
    </FormPageLayout>
  );
};

export default AddUnit;
