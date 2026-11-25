import { usePageMeta } from '@/lib/usePageMeta';
import { PageHero } from '@/components/layout/PageHero';
import { FeatureExplorer } from '@/sections/features/FeatureExplorer';
import { SafeByDesign } from '@/sections/features/SafeByDesign';
import { Versus } from '@/sections/features/Versus';

export default function Features() {
  usePageMeta('/features');
  return (
    <>
      <PageHero
        eyebrow="Features"
        title="Everything you need to run the billing counter"
        lead="Click through the screens below. Each one works the way it does in MintoBill."
      />
      <FeatureExplorer />
      <Versus />
      <SafeByDesign />
    </>
  );
}
