import React, { useState, useEffect } from 'react';
import { TrendingUp, ShoppingBag, Users, Package, Banknote, CheckCircle2, Clock, AlertCircle } from 'lucide-react';
import { Bar, Pie } from 'react-chartjs-2';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
  ArcElement,
} from 'chart.js';
import { api } from '../services/api';
import { PageWrapper } from '@/components/layout/AppShell';
import { PageHeader } from '@/components/shared/page-header';
import { KpiCard } from '@/components/shared/kpi-card';
import { SectionCard } from '@/components/shared/section-card';
import { Card, CardContent } from '@/components/ui/card';
import { cn } from '@/lib/utils';
import { formatCurrency, currencySymbol, formatNumber } from '@/lib/format';

ChartJS.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend, ArcElement);

export default function Dashboard() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [activeChart, setActiveChart] = useState('revenue');

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const dashboardData = await api.get('/api/dashboard');
        setData(dashboardData);
      } catch (err) {
        setError(err.message || 'Failed to fetch dashboard data');
        console.error(err);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  if (loading) {
    return (
      <PageWrapper>
        <div className="flex items-center justify-center py-20 text-muted-foreground">Loading dashboard...</div>
      </PageWrapper>
    );
  }

  if (error) {
    return (
      <PageWrapper>
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-destructive">{error}</div>
      </PageWrapper>
    );
  }

  const barChartData = {
    labels: data?.revenueData.map((d) => d.month) || [],
    datasets: [
      {
        label: activeChart === 'revenue' ? 'Revenue' : 'Orders',
        data: data?.revenueData.map((d) => d[activeChart]) || [],
        backgroundColor: 'rgba(16, 185, 129, 0.75)',
        borderRadius: 6,
      },
    ],
  };

  const barChartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        callbacks: {
          label: (context) => {
            let label = context.dataset.label || '';
            if (label) label += ': ';
            if (context.parsed.y !== null) {
              label += activeChart === 'revenue' ? formatCurrency(context.parsed.y) : context.parsed.y;
            }
            return label;
          },
        },
      },
    },
    scales: {
      y: {
        beginAtZero: true,
        ticks: {
          callback: (value) => (activeChart === 'revenue' ? `${currencySymbol()}${formatNumber(value, 0)}` : value),
        },
      },
    },
  };

  const pieChartData = {
    labels: data?.salesDistribution.map((d) => d.category) || [],
    datasets: [
      {
        data: data?.salesDistribution.map((d) => d.value) || [],
        backgroundColor: data?.salesDistribution.map((d) => d.color) || [],
        borderColor: '#fff',
        borderWidth: 2,
      },
    ],
  };

  const pieChartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: {
        position: 'bottom',
        labels: {
          boxWidth: 10,
          padding: 10,
          font: { size: 11 },
          usePointStyle: true,
        },
      },
      tooltip: {
        callbacks: {
          label: (context) => {
            let label = context.label || '';
            if (label) label += ': ';
            if (context.parsed !== null) label += formatCurrency(context.parsed);
            return label;
          },
        },
      },
    },
  };

  return (
    <PageWrapper>
      <PageHeader
        hideTitle
        description={`Welcome back, ${data?.currentUser || 'Admin'}. Overview of your store performance.`}
        actions={
          <div className="flex flex-wrap items-center gap-1 rounded-lg border bg-card p-1">
            {['revenue', 'orders'].map((r) => (
              <button
                key={r}
                onClick={() => setActiveChart(r)}
                className={cn(
                  'rounded-md px-3 py-1.5 text-xs font-medium capitalize transition-colors',
                  activeChart === r
                    ? 'bg-primary text-primary-foreground shadow-xs font-semibold'
                    : 'text-muted-foreground hover:bg-accent hover:text-foreground'
                )}
              >
                {r}
              </button>
            ))}
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard label="Total Revenue" value={formatCurrency(data.stats.totalRevenue)} icon={TrendingUp} accent="emerald" />
        <KpiCard label="Total Orders" value={String(data.stats.totalOrders)} icon={ShoppingBag} accent="sky" />
        <KpiCard label="Total Customers" value={String(data.stats.totalCustomers)} icon={Users} accent="violet" />
        <KpiCard label="Total Products" value={String(data.stats.totalProducts)} icon={Package} accent="amber" />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard label="Total Amount" value={formatCurrency(data.summaryCards.totalAmount)} icon={Banknote} accent="sky" />
        <KpiCard label="Total Paid" value={formatCurrency(data.summaryCards.totalPaid)} icon={CheckCircle2} accent="emerald" />
        <KpiCard label="Total Unpaid" value={formatCurrency(data.summaryCards.totalUnpaid)} icon={Clock} accent="amber" />
        <KpiCard label="Total Overdue" value={formatCurrency(data.summaryCards.totalOverdue)} icon={AlertCircle} accent="rose" />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <SectionCard title="Revenue Overview" className="lg:col-span-2" contentClassName="p-4 sm:p-6 pt-0">
          <div className="h-[340px] w-full">
            <Bar data={barChartData} options={barChartOptions} />
          </div>
        </SectionCard>

        <SectionCard title="Sales Distribution" contentClassName="p-4 sm:p-6 pt-0">
          <div className="h-[340px] w-full flex items-center justify-center">
            <Pie data={pieChartData} options={pieChartOptions} />
          </div>
        </SectionCard>
      </div>
    </PageWrapper>
  );
}
