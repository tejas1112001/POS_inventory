import bcrypt from 'bcrypt';
import { User } from './src/models/index.js';

async function resetPasswords() {
  const hashAdmin = await bcrypt.hash('admin123', 10);
  const hashUser = await bcrypt.hash('123456', 10);

  await User.update({ password: hashAdmin }, { where: { email: 'admin@pos.com' } });
  await User.update({ password: hashUser }, { where: { email: 'tejas@gmail.com' } });

  console.log('Passwords updated successfully!');
}

resetPasswords().then(() => process.exit(0)).catch(err => {
  console.error(err);
  process.exit(1);
});
