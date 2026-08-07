import express from 'express';
import { sequelize, Sale, Customer, Product, OrderItem } from '../models/index.js';
import { QueryTypes } from 'sequelize';

const router = express.Router();

router.get('/', async (req, res) => {
  try {
    const [totalOrders, totalCustomers, totalProducts] = await Promise.all([
      Sale.count(),
      Customer.count(),
      Product.count(),
    ]);

    // Aggregations via Sequelize raw queries for clarity
    const [revenueRow] = await sequelize.query(
      `SELECT COALESCE(SUM(total), 0) AS "totalRevenue" FROM sales`,
      { type: QueryTypes.SELECT }
    );

    const [paidRow] = await sequelize.query(
      `SELECT COALESCE(SUM(paid), 0) AS "totalPaid" FROM sales WHERE payment_status = 'Paid'`,
      { type: QueryTypes.SELECT }
    );

    const [unpaidRow] = await sequelize.query(
      `SELECT COALESCE(SUM(due), 0) AS "totalUnpaid" FROM sales WHERE payment_status = 'Unpaid'`,
      { type: QueryTypes.SELECT }
    );

    const [overdueRow] = await sequelize.query(
      `SELECT COALESCE(SUM(due), 0) AS "totalOverdue" FROM sales WHERE payment_status = 'Overdue'`,
      { type: QueryTypes.SELECT }
    );

    // Monthly revenue chart data
    const recentSales = await Sale.findAll({
      attributes: ['date', 'total'],
      order: [['date', 'ASC']],
      limit: 100,
    });

    const monthlyData = {};
    recentSales.forEach((sale) => {
      if (!sale.date) return;
      const month = new Date(sale.date).toLocaleString('default', { month: 'short' });
      if (!monthlyData[month]) monthlyData[month] = { revenue: 0, orders: 0 };
      monthlyData[month].revenue += sale.total || 0;
      monthlyData[month].orders += 1;
    });

    const revenueData = Object.entries(monthlyData).map(([month, data]) => ({
      month,
      revenue: data.revenue,
      orders: data.orders,
    }));

    // Top 5 products by revenue
    const topProducts = await sequelize.query(
      `SELECT p.name AS category, COALESCE(SUM(oi.subtotal), 0) AS value
       FROM order_items oi
       JOIN products p ON p.id = oi.product_id
       GROUP BY p.id, p.name
       ORDER BY value DESC
       LIMIT 5`,
      { type: QueryTypes.SELECT }
    );

    const colors = ['#4F46E5', '#10B981', '#F59E0B', '#EF4444', '#8B5CF6'];
    const salesDistribution = topProducts.map((item, i) => ({
      ...item,
      color: colors[i % colors.length],
    }));

    res.json({
      stats: {
        totalRevenue: Number(revenueRow?.totalRevenue || 0),
        totalOrders,
        totalCustomers,
        totalProducts,
      },
      revenueData,
      salesDistribution,
      summaryCards: {
        totalAmount: Number(revenueRow?.totalRevenue || 0),
        totalPaid: Number(paidRow?.totalPaid || 0),
        totalUnpaid: Number(unpaidRow?.totalUnpaid || 0),
        totalOverdue: Number(overdueRow?.totalOverdue || 0),
      },
    });
  } catch (error) {
    console.error('GET /api/dashboard error:', error);
    res.status(500).json({ error: 'Failed to fetch dashboard data' });
  }
});

export default router;
