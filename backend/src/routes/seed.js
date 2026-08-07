/**
 * One-time database seed script.
 * Run directly: node src/routes/seed.js
 * Creates a default store and seeds inventory for all existing product variants.
 */
import dotenv from 'dotenv';
dotenv.config();

import { connectDB, default as sequelize } from '../config/db.js';
import { Store, ProductVariant, Inventory } from '../models/index.js';

const STORE_NAME = 'islamabad-main';
const DEFAULT_QTY = 100;

const seedDatabase = async () => {
  await connectDB();
  const t = await sequelize.transaction();

  try {
    console.log('Seeding database...');

    // Find or create the main store
    let [store, created] = await Store.findOrCreate({
      where: { name: STORE_NAME },
      defaults: { name: STORE_NAME, address: 'Islamabad', status: 'Active' },
      transaction: t,
    });
    console.log(`Store "${STORE_NAME}" ${created ? 'created' : 'already exists'} — ID: ${store.id}`);

    // Get all product variants
    const variants = await ProductVariant.findAll({ transaction: t });
    console.log(`Found ${variants.length} product variants.`);

    // Upsert inventory for each variant
    for (const variant of variants) {
      const existing = await Inventory.findOne({
        where: { variant_id: variant.id, store_id: store.id },
        transaction: t,
      });

      if (!existing) {
        await Inventory.create(
          { variant_id: variant.id, store_id: store.id, qty: DEFAULT_QTY, quantity_alert: 10 },
          { transaction: t }
        );
      }
    }

    await t.commit();
    console.log(`Seeding complete! Inventory set for ${variants.length} variants.`);
  } catch (error) {
    await t.rollback();
    console.error('Seeding failed:', error);
  } finally {
    await sequelize.close();
  }
};

seedDatabase();
