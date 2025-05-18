import { Component, ViewChild, ElementRef, OnInit, OnDestroy } from '@angular/core';
import { WebrtcService } from './webrtc.service';
import { Subscription } from 'rxjs';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.css']
})
export class AppComponent implements OnInit, OnDestroy {
  title = 'WebRTC Screen Share Angular';
  @ViewChild('remoteVideo') videoElementRef!: ElementRef<HTMLVideoElement>;
  private streamSubscription: Subscription | undefined;

  constructor(private webrtcService: WebrtcService) { }

  ngOnInit() {
    this.streamSubscription = this.webrtcService.remoteStream$.subscribe(stream => {
      if (this.videoElementRef && this.videoElementRef.nativeElement) {
        this.videoElementRef.nativeElement.srcObject = stream;
        if (stream) {
          console.log("Remote video stream attached to video element");
        } else {
          console.log("Remote video stream detached from video element");
        }
      }
    });
  }

  ngOnDestroy() {
    this.streamSubscription?.unsubscribe();
  }

  startStream() {
    console.log('Start button clicked');
    this.webrtcService.start();
  }

  stopStream() {
    console.log('Stop button clicked');
    this.webrtcService.stop();
  }
}
