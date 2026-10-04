import { Sequelize } from 'sequelize';
import dotenv from 'dotenv';
dotenv.config();

const {
  DB_HOST,
  DB_PORT = '5432',
  DB_NAME,
  DB_USER,
  DB_PASSWORD,
  DB_SSL = 'false',
  NODE_ENV,
} = process.env;

if (!DB_HOST || !DB_NAME || !DB_USER || !DB_PASSWORD) {
  console.error('Missing required database environment variables: DB_HOST, DB_NAME, DB_USER, DB_PASSWORD');
  process.exit(1);
}

const useSSL = DB_SSL === 'true';

const sequelize = new Sequelize(DB_NAME, DB_USER, DB_PASSWORD, {
  host: DB_HOST,
  port: parseInt(DB_PORT, 10),
  dialect: 'postgres',
  logging: NODE_ENV === 'development' ? console.log : false,
  dialectOptions: useSSL
    ? { ssl: { require: true, rejectUnauthorized: false } }
    : {},
  pool: {
    max: parseInt(process.env.DB_POOL_MAX || '25', 10),
    min: parseInt(process.env.DB_POOL_MIN || '5', 10),
    acquire: parseInt(process.env.DB_POOL_ACQUIRE || '30000', 10),
    idle: parseInt(process.env.DB_POOL_IDLE || '10000', 10),
  },
});

export const connectDB = async () => {
  try {
    await sequelize.authenticate();
    console.log('PostgreSQL connected');
    // sync({ alter: true }) updates columns without dropping data — safe for dev.
    // Switch to migrations for production.
    await sequelize.sync({ alter: true });
    console.log('Database synced');
  } catch (error) {
    console.error('PostgreSQL connection error:', error);
    process.exit(1);
  }
};

export default sequelize;
