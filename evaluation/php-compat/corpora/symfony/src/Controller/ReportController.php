<?php

declare(strict_types=1);

namespace App\Controller;

use App\Service\ReportService;
use Symfony\Bundle\FrameworkBundle\Controller\AbstractController;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;

final class ReportController extends AbstractController
{
    #[Route('/reports', name: 'report_index')]
    public function index(ReportService $reports): Response
    {
        return $this->render('report/index.html.twig', [
            'reports' => $reports->recent(),
        ]);
    }
}
