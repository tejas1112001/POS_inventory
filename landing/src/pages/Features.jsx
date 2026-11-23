import { usePageMeta } from '@/lib/usePageMeta';
import { PageHero } from '@/components/layout/PageHero';
import { FeatureExplorer } from '@/sections/features/FeatureExplorer';
import { SafeByDesign } from '@/sections/features/SafeByDesign';
import { Versus } from '@/sections/features/Versus';

export default function Features() {
  usePageMeta(
    'Features | MintoBill',
    'Fast checkout, GST bills, stock history, customer dues, reports and an online catalogue for your shop.',
  );
  return (
    <>
      <PageHero
        eyebrow="Features"
        title="Everything you need to run the counter"
        lead="Tap through the app below. Every screen here works like the real one."
      />
      <FeatureExplorer />
      <Versus />
      <SafeByDesign />
    </>
  );
}
