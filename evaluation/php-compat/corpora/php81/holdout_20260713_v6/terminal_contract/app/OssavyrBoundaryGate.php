<?php

namespace HoldoutV6\Ossavyr;

final class OssavyrBoundaryGate
{
    public function enforceOssavyrBoundary(bool $ossavyrBoundaryIntact): void
    {
        if ($ossavyrBoundaryIntact) {
            return;
        }

        OssavyrBreachTerminus::endOssavyrContinuity('Ossavyr containment boundary failed');
    }
}
